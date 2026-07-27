# Yatagarasu

Self-hosted shelf that carries manga chapters from [Karasu](https://github.com/clevim/Karasu)
(Android manga reader) to **KOReader** on an e-reader, and read state back.

**八咫烏** — the three-legged crow that guides between worlds. Karasu (烏) pushes chapters here;
the KOReader plugin picks them up and reports back what was finished.

```
┌──────────┐   CBZ + metadata (POST)   ┌───────────┐   plugin downloads CBZ   ┌──────────┐
│  Karasu  │ ────────────────────────► │   shelf   │ ───────────────────────► │ KOReader │
│ (Android)│ ◄──────────────────────── │(container)│ ◄─────────────────────── │ (plugin) │
└──────────┘   read state (GET)        └───────────┘   "finished" is reported  └──────────┘
```

Karasu and the e-reader never talk to each other, so the tablet can be offline whenever Karasu
happens to sync.

## Run it

```yaml
# docker-compose.yml
services:
  yatagarasu:
    image: ghcr.io/clevim/yatagarasu:latest
    restart: unless-stopped
    ports: ["8080:8080"]
    volumes: ["./data:/data"]
    environment:
      YATA_API_KEY: "change-me"
```

```sh
docker compose up -d
```

Then in Karasu: **Settings → Yatagarasu**, shelf address `http://<host>:8080`, the same API key,
pick the categories to sync, and press *Test connection*.

| Variable | Default | Meaning |
| --- | --- | --- |
| `YATA_API_KEY` | *(blank)* | Bearer token. **Blank means unauthenticated requests are accepted** — fine on a trusted LAN, nowhere else. |
| `YATA_PUBLIC_URL` | *(request host)* | The URL clients see. Set it behind a reverse proxy; it is baked into the generated plugin. |
| `TZ` | `UTC` | Timezone for the timestamps on the web page. |
| `YATA_DATA` | `/data` | Where CBZ files and metadata live. |
| `YATA_ADDR` | `:8080` | Listen address. |

### Settings

The first four are first-run defaults. **Settings** on the web page changes them without recreating
the container, and writes `/data/settings.json`, which wins from then on — editing the environment
afterwards does nothing.

- **Public URL and API key** — both are baked into the plugin ZIP at the moment it is downloaded, so
  fixing them here and re-downloading is the whole repair for a plugin that cannot reach the shelf.
  A key change takes effect on the next request; Karasu and any installed plugin need the new value.
- **Timezone** — the container runs on UTC, which puts the activity chart's day boundary at 21:00 in
  Brazil and labels every bar a day early.
- **Activity chart window** — 7, 14 or 30 days.
- **Danger zone** — clear the reading history, or empty the shelf. Neither touches anything already
  downloaded to the e-reader; Karasu re-uploads whatever it still wants on its next sync.

Individual chapters have a **remove** button on the shelf page, next to **undo**.

### FlareSolverr (optional)

Nothing here needs it — it is in the compose file because Karasu does, and this is the machine that
is already on. It carries a headless Chrome, so it stays off unless asked for:

```sh
docker compose --profile flaresolverr up -d
```

Then in Karasu: **Settings → Advanced → FlareSolverr URL**, `http://<host>:8191`.

## Install the KOReader plugin

Open `http://<host>:8080/` in a browser and click **Download the KOReader plugin**. The ZIP is
generated per request and already contains this server's address and API key, so nothing has to be
typed on an e-ink keyboard.

Unzip it into `koreader/plugins/` on the device (you should end up with
`koreader/plugins/yata.koplugin/main.lua`) and restart KOReader. The menu entry is
**Tools → Yatagarasu**:

- **Sync now** — reports finished chapters, then downloads whatever is new. Tap the progress
  message to stop after the current chapter.
- **Sync automatically** — off, every 6 h, every 12 h, or once a day. It only runs when the Wi-Fi
  is already on: a timer that switches the radio on by itself is how a two-week battery becomes a
  two-day one. Finished chapters are reported the moment Wi-Fi comes back either way.

Downloads run in a subprocess, so the reader keeps painting and turning pages through a 60 MB
chapter. A tap cancels the transfer in flight and still turns the page; the partial file stays on
disk and the next sync resumes from it.
- **Shelf address and API key** — only needed if you copied the plugin by hand.
- **Download folder** — where the CBZ files land, as `<folder>/Karasu/<manga>/<chapter>.cbz`.
- **Remove finished downloads** — deletes only chapters the shelf has already acknowledged as read.
- **Screen width** — this e-reader's width in pixels. Type it into Karasu under *Settings →
  Yatagarasu → Screen width*: Karasu splits and scales pages before uploading, but it has no way to
  know how wide this screen is.

Syncing is strictly additive: a chapter leaving the shelf never deletes the local file, because
Karasu also drops entries for reasons that have nothing to do with the reader (its manga/chapter
limits). Deleting is always an explicit action.

Interrupted downloads resume from where they stopped, and a chapter whose size does not match what
the shelf reports is discarded instead of landing in the library. A chapter that would not fit in
the free space is skipped and counted in the sync summary.

## Troubleshooting

**The shelf stays empty and the connection test is green.** Karasu only uploads chapters that are
already downloaded **as CBZ**. Turn on *Save chapters as CBZ* in Karasu's download settings, and
make sure some categories are selected under *Settings → Yatagarasu*. `GET /api/health` reports the
entry count, so a green test showing `"entries":0` is the expected symptom.

**A webtoon chapter is an unreadable smear on the e-reader.** Pages are reshaped by Karasu before
upload, not here — turn on *Split long pages* under *Settings → Yatagarasu* and re-sync the chapter.
The shelf stores whatever arrives, so a chapter already uploaded has to be re-sent to change.

**A chapter was read on the device but Karasu did not mark it.** Read reports are queued on disk and
retried on the next sync, so run *Sync now* with Wi-Fi on. If it still does not stick, the chapter
url no longer matches — that happens after restoring a Karasu backup, which renumbers chapter ids.
Those stale entries get pruned and re-uploaded on the next sync.

## API

```
GET    /api/health                    Karasu — "Test connection"
GET    /api/shelf                     Karasu + plugin
POST   /api/shelf                     Karasu — multipart metadata + file, upsert by chapterId
DELETE /api/shelf/{chapterId}         Karasu — 204 even when absent
GET    /api/shelf/{chapterId}/file    plugin — CBZ, supports Range
POST   /api/shelf/{chapterId}/read    plugin — idempotent, {"read":false} undoes it
GET    /plugin.zip                    browser — the plugin, preconfigured
GET    /settings, POST /settings      browser — the settings page
POST   /api/shelf/{chapterId}/delete  browser — the remove button; forms cannot send DELETE
```

The first four are the frozen contract with Karasu, specified in `docs/koreader-sync.md` in the
[Karasu repo](https://github.com/clevim/Karasu) — that copy is the source of truth, and Karasu will
not change to accommodate this server.

## Storage

No database. One `<chapterId>.cbz` plus one `<chapterId>.json` per entry, under `/data`. The shelf
is bounded by Karasu's own limits (default 10 manga × 3 chapters), so listing it is a glob and a few
dozen small reads.

## Develop

```sh
go test ./...          # 18 tests, no dependencies
lua api_test.lua       # the plugin's download path; go test runs it too
go run .               # YATA_DATA=./data YATA_API_KEY= go run .
```

`TestChapterURLSurvivesRoundTrip` is the contract's enforcement: Karasu refuses to mark a chapter
read unless `chapterUrl` comes back byte for byte. Do not weaken it.

## License

MIT.
