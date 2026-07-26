-- HTTP calls against the Yatagarasu shelf.
--
-- Shape copied from KOReader's own OPDS browser and from KoInsight's plugin:
-- socket.http handles both http and https in KOReader, socketutil sets the
-- timeouts, ltn12 sinks stream to a table (JSON) or straight to a file (CBZ).

local http = require("socket.http")
local lfs = require("libs/libkoreader-lfs")
local ltn12 = require("ltn12")
local logger = require("logger")
local socket = require("socket")
local socketutil = require("socketutil")
local JSON = require("json")

local Api = {}
Api.__index = Api

function Api:new(o)
  return setmetatable(o or {}, self)
end

function Api:url(path)
  return (self.base_url or ""):gsub("/+$", "") .. path
end

function Api:headers(extra)
  local h = extra or {}
  if self.api_key and self.api_key ~= "" then
    h["Authorization"] = "Bearer " .. self.api_key
  end
  return h
end

-- shelf() -> entries table, or nil plus a message.
function Api:shelf()
  local sink = {}
  socketutil:set_timeout(socketutil.LARGE_BLOCK_TIMEOUT, socketutil.LARGE_TOTAL_TIMEOUT)
  local code, _headers, status = socket.skip(1, http.request({
    url = self:url("/api/shelf"),
    method = "GET",
    headers = self:headers(),
    sink = ltn12.sink.table(sink),
  }))
  socketutil:reset_timeout()

  if code ~= 200 then
    logger.err("yata: GET /api/shelf failed:", status or code)
    return nil, tostring(status or code)
  end
  local ok, body = pcall(JSON.decode, table.concat(sink))
  if not ok or type(body) ~= "table" then
    logger.err("yata: shelf response is not JSON")
    return nil, "invalid response"
  end
  return body.entries or {}
end

-- download() writes to a .part file and renames, so an interrupted download
-- never leaves a half CBZ sitting in the library.
--
-- E-reader Wi-Fi drops mid-chapter and chapters are routinely 60 MB, so a
-- half file is kept and resumed with a Range request on the next sync. The
-- shelf's size for the chapter is what makes that safe: it says when the part
-- is stale (the chapter was re-uploaded) and when the finished file is short.
function Api:download(chapter_id, path, size)
  local part = path .. ".part"
  local have = lfs.attributes(part, "size") or 0
  if not size or size <= 0 or have >= size then
    have = 0 -- unknown size, or a leftover from a different upload: start over
  end

  local f = io.open(part, have > 0 and "ab" or "wb")
  if not f then
    logger.err("yata: cannot write", part)
    return false
  end

  socketutil:set_timeout(socketutil.FILE_BLOCK_TIMEOUT, socketutil.FILE_TOTAL_TIMEOUT)
  local code, _headers, status = socket.skip(1, http.request({
    url = self:url("/api/shelf/" .. tostring(chapter_id) .. "/file"),
    method = "GET",
    headers = self:headers({
      ["Accept-Encoding"] = "identity",
      ["Range"] = have > 0 and ("bytes=" .. have .. "-") or nil,
    }),
    sink = ltn12.sink.file(f), -- sink.file closes the handle
  }))
  socketutil:reset_timeout()

  -- 200 to a Range request means something between here and the shelf ignored
  -- it and re-sent the whole body, which just got appended to what we had.
  if have > 0 and code == 200 then
    logger.warn("yata: Range ignored for", chapter_id, "- restarting the download")
    os.remove(part)
    return self:download(chapter_id, path, size)
  end
  if code ~= 200 and code ~= 206 then
    logger.err("yata: download of", chapter_id, "failed:", status or code)
    return false -- keep the part file: the next sync resumes from here
  end

  -- A connection dropped at the right moment ends a body cleanly, so size is
  -- the only thing standing between a truncated CBZ and the library.
  local got = lfs.attributes(part, "size") or 0
  if size and size > 0 and got ~= size then
    logger.err("yata: download of", chapter_id, "is", got, "bytes, shelf says", size)
    return false
  end
  return os.rename(part, path) and true or false
end

-- markRead() is idempotent server-side, so re-reporting costs nothing.
function Api:markRead(chapter_id)
  local sink = {}
  socketutil:set_timeout(socketutil.LARGE_BLOCK_TIMEOUT, socketutil.LARGE_TOTAL_TIMEOUT)
  local code, _headers, status = socket.skip(1, http.request({
    url = self:url("/api/shelf/" .. tostring(chapter_id) .. "/read"),
    method = "POST",
    headers = self:headers({ ["Content-Length"] = "0" }),
    sink = ltn12.sink.table(sink),
  }))
  socketutil:reset_timeout()

  -- 404 means the entry is already gone from the shelf (Karasu pruned it):
  -- nothing left to tell, so stop retrying it forever.
  if code == 404 then
    return true
  end
  if type(code) ~= "number" or code < 200 or code > 299 then
    logger.err("yata: read report for", chapter_id, "failed:", status or code)
    return false
  end
  return true
end

return Api
