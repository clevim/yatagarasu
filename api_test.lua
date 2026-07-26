-- Offline check for Api:download in yata.koplugin/api.lua.
--
-- Resume, a Range the server ignored, and a body that arrived short are the
-- three branches standing between a dropped Wi-Fi and a corrupt CBZ in the
-- library, and none of them are observable without sideloading otherwise.
-- KOReader's modules are stubbed just enough to load api.lua.
--
-- Run: lua api_test.lua   (also runs from `go test ./...`)

package.path = "yata.koplugin/?.lua;" .. package.path

local script, sent -- queued responses, and the headers of the last request

package.preload["socket.http"] = function()
  return {
    request = function(req)
      sent = req.headers
      local step = table.remove(script, 1)
      assert(step, "http.request called more times than the test scripted")
      if step.body then
        req.sink(step.body)
      end
      req.sink(nil) -- end of stream: ltn12 sinks close on nil
      return 1, step.code, {}, "stub"
    end,
  }
end

package.preload["ltn12"] = function()
  return {
    sink = {
      file = function(f)
        return function(chunk)
          if chunk then
            f:write(chunk)
          else
            f:close()
          end
          return 1
        end
      end,
      table = function(t)
        return function(chunk)
          if chunk then
            t[#t + 1] = chunk
          end
          return 1
        end
      end,
    },
  }
end

package.preload["socket"] = function()
  return {
    skip = function(n, ...)
      return select(n + 1, ...)
    end,
  }
end

package.preload["socketutil"] = function()
  return setmetatable({
    set_timeout = function() end,
    reset_timeout = function() end,
  }, {
    __index = function()
      return 0
    end,
  })
end

package.preload["logger"] = function()
  return setmetatable({}, {
    __index = function()
      return function() end
    end,
  })
end

package.preload["json"] = function()
  return { decode = function() end }
end

package.preload["libs/libkoreader-lfs"] = function()
  return {
    attributes = function(path, what)
      local f = io.open(path, "rb")
      if not f then
        return nil
      end
      local size = f:seek("end")
      f:close()
      if what == "size" then
        return size
      end
      return what == "mode" and "file" or { size = size, mode = "file" }
    end,
  }
end

local Api = require("api")

-- ---- helpers ---------------------------------------------------------------

local dir = (os.getenv("TMPDIR") or "/tmp") .. "/yata-api-test"
os.execute("rm -rf '" .. dir .. "' && mkdir -p '" .. dir .. "'")

local CBZ = "PK\003\004chapter-bytes"
local path = dir .. "/chapter.cbz"
local part = path .. ".part"

local function read(p)
  local f = io.open(p, "rb")
  if not f then
    return nil
  end
  local body = f:read("*a")
  f:close()
  return body
end

local function write(p, body)
  local f = assert(io.open(p, "wb"))
  f:write(body)
  f:close()
end

local function reset()
  os.remove(path)
  os.remove(part)
  sent = nil
end

local api = Api:new({ base_url = "http://shelf", api_key = "k" })

-- ---- a whole chapter in one go --------------------------------------------

reset()
script = { { code = 200, body = CBZ } }
assert(api:download(1, path, #CBZ), "a complete download should succeed")
assert(read(path) == CBZ, "the file on disk is not what the shelf sent")
assert(read(part) == nil, "the .part file should be gone after the rename")
assert(sent["Range"] == nil, "nothing to resume: no Range header")

-- ---- a body that stopped early --------------------------------------------

reset()
script = { { code = 200, body = CBZ:sub(1, 6) } }
assert(not api:download(1, path, #CBZ), "a short body must not count as a download")
assert(read(path) == nil, "a truncated chapter must never reach the library")
assert(read(part) == CBZ:sub(1, 6), "the partial bytes are kept for the next sync")

-- ---- resuming from what the last attempt left ------------------------------

script = { { code = 206, body = CBZ:sub(7) } } -- .part still holds the first 6 bytes
assert(api:download(1, path, #CBZ), "resuming should succeed")
assert(sent["Range"] == "bytes=6-", "expected a Range request, got " .. tostring(sent["Range"]))
assert(read(path) == CBZ, "resume produced the wrong bytes")

-- ---- a proxy that ignored the Range ----------------------------------------

reset()
write(part, CBZ:sub(1, 6))
script = {
  { code = 200, body = CBZ }, -- whole body despite the Range: restart
  { code = 200, body = CBZ },
}
assert(api:download(1, path, #CBZ), "an ignored Range should restart, not fail")
assert(read(path) == CBZ, "restart after an ignored Range doubled or lost bytes")

-- ---- a stale .part from a different upload ---------------------------------

reset()
write(part, string.rep("x", #CBZ + 10)) -- bigger than the shelf says: not ours
script = { { code = 200, body = CBZ } }
assert(api:download(1, path, #CBZ), "a stale .part should be replaced")
assert(sent["Range"] == nil, "a stale .part must not be resumed")
assert(read(path) == CBZ, "stale bytes survived into the library")

os.execute("rm -rf '" .. dir .. "'")
print("api.lua: 5 download cases ok")
