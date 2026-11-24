<p align="center">
  <img src="assets/icon.png" alt="" width="132">
</p>

<h1 align="center">Yatagarasu</h1>

<p align="center">
  Self-hosted companion server for
  <a href="https://github.com/clevim/Karasu">Karasu</a> (Android manga reader) —
  web library &amp; reader, 24/7 Suwayomi bridge, shared translation, backups,
  bidirectional sync, and a <b>KOReader</b> e-reader shelf.
</p>

**八咫烏** — the three-legged crow that guides between worlds. Karasu (烏) syncs library state,
backups, and chapters here; you can read from any browser, queue 24/7 downloads, translate pages,
and let the KOReader plugin pick up chapters and report back what was finished.

```
┌──────────┐  sync, backups, CBZ   ┌────────────┐   plugin downloads CBZ   ┌──────────┐
│  Karasu  │ ◄───────────────────► │ Yatagarasu │ ───────────────────────► │ KOReader │
│ (Android)│  read state & stream  │  (:3080)   │ ◄─────────────────────── │ (plugin) │
└──────────┘                       └─────┬──────┘   "finished" is reported └──────────┘
                                         │
                        ┌────────────────┼────────────────┐
                        ▼                ▼                ▼
                   Web UI / PC      Suwayomi-Server  Image / LLM
                     Reader         (extensions/DL)   Translator
```

Karasu and the e-reader never talk to each other, so the tablet can be offline whenever Karasu
happens to sync.

## Companion v2 features

- **PC Library Grid** — browse your synced Karasu library by category, search/filter titles, and
  manage entries from any desktop or tablet browser.
- **Updates & History** — track newly released chapters and view unified reading history across
  Karasu, the web reader, and KOReader.
- **Browse, Extensions & Source Prefs** — search online catalogs through Suwayomi, install, update,
  or remove Mihon/Tachiyomi extensions, and tweak per-source preferences directly in the web UI.
- **Built-in Reader with Live Page Translation** — read chapters in the browser (RTL, LTR, webtoon)
  with on-demand full-page visual translation (OCR, LaMa inpainting, and typesetting) plus shared
  LLM text translation and per-series glossaries.
- **Downloads** — queue and monitor 24/7 chapter downloads as CBZ via Suwayomi and stream them to
  Karasu over LAN.
- **Backups** — upload, browse, download, and manage `.tachibk` Karasu backups.
- **KOReader Shelf (with optional translation burn-in)** — serve CBZ chapters to the KOReader
  plugin, optionally burning translated pages directly into the CBZ before the e-reader fetches
  them, and report finished chapters back.
- **Bidirectional Karasu Sync** — reconcile library entries, categories, reading progress, merged
  sources, and extension repositories between Karasu and Yatagarasu.

## Run it

Port **3080** is 三本足 + 80 — the crow's three legs on the HTTP port. It stays clear of the usual
homelab occupants (8080, 8096 Jellyfin, 8123 Home Assistant, 9000 Portainer).

### Option 1: All-in-One single container

Bundles **Yatagarasu** and the official **Suwayomi-Server** behind a single `:3080` front door,
with all state persisted under `/data` (`/data/yatagarasu`, `/data/suwayomi`, and
`/data/translator-models`). Suwayomi stays strictly on loopback inside the container.

Pull the prebuilt multi-arch image (`linux/amd64`, `linux/arm64`):

```yaml
services:
  yatagarasu:
    image: ghcr.io/clevim/yatagarasu:allinone
    restart: unless-stopped
    ports: ["3080:3080"]
    volumes: ["./data:/data"]
    environment:
      YATA_API_KEY: "change-me"
      TZ: America/Sao_Paulo
```

Or build and run locally with [`docker-compose.allinone.yml`](docker-compose.allinone.yml):

```sh
docker compose -f docker-compose.allinone.yml up -d
```

To also bundle [`zyddnys/manga-image-translator`](https://github.com/zyddnys/manga-image-translator)
inside the same container (OCR + LaMa inpainting + typesetting on internal loopback `:8000`), build
with `WITH_IMAGE_TRANSLATOR=true`:

```sh
docker build -f Dockerfile.allinone --build-arg WITH_IMAGE_TRANSLATOR=true -t yatagarasu:allinone-ml .
```

Or set `WITH_IMAGE_TRANSLATOR: "true"` under `build.args` in `docker-compose.allinone.yml`. It is
disabled by default so the standard `:allinone` image stays compact (~450 MB).

### Option 2: Lean container & modular Compose profiles

If you only want the lightweight `scratch` binary (~15 MB) or prefer running sidecars as separate
containers, use [`docker-compose.yml`](docker-compose.yml):

```yaml
# docker-compose.yml (minimal)
services:
  yatagarasu:
    image: ghcr.io/clevim/yatagarasu:latest
    restart: unless-stopped
    ports: ["3080:3080"]
    volumes: ["./data:/data"]
    environment:
      YATA_API_KEY: "change-me"
```

```sh
docker compose up -d
```

Optional sidecars are gated behind Docker Compose profiles so they stay off unless requested:

| Profile | Service | Purpose |
| --- | --- | --- |
| `--profile suwayomi` | `ghcr.io/suwayomi/suwayomi-server:stable` | Official Suwayomi-Server for 24/7 Mihon/Tachiyomi extensions and CBZ downloads (`YATA_SUWAYOMI_URL=http://suwayomi:4567`). |
| `--profile translator` | `zyddnys/manga-image-translator:main` | Internal-only full-page visual translation sidecar for the web reader and KOReader burn-in (`YATA_IMAGE_TRANSLATOR_URL=http://manga-translator:8000`). |
| `--profile flaresolverr` | `ghcr.io/flaresolverr/flaresolverr:latest` | Headless Chrome Cloudflare bypass for Karasu and Suwayomi (`http://<host>:8191`). |

Enable any combination of profiles:

```sh
docker compose --profile suwayomi --profile translator --profile flaresolverr up -d
```

Then in Karasu:
- **Settings → Yatagarasu**: shelf address `http://<host>:3080`, the same API key, pick categories
  to sync, and press *Test connection*.
- **Settings → Advanced → FlareSolverr URL** (if enabled): `http://<host>:8191`.

### Environment variables

| Variable | Default | Meaning |
| --- | --- | --- |
| `YATA_API_KEY` | *(blank)* | Bearer token. **Blank means unauthenticated requests are accepted** — fine on a trusted LAN, nowhere else. Generate one with `openssl rand -base64 24`; the server warns at startup if it is short or blank. |
| `YATA_PUBLIC_URL` | *(request host)* | The URL clients see. Set it behind a reverse proxy; it is baked into the generated plugin. |
| `TZ` | `UTC` | Timezone for the timestamps on the web page. |
| `YATA_DATA` | `/data` | Where CBZ files, backups, sync state, and metadata live (`/data/yatagarasu` in `:allinone`). |
| `YATA_ADDR` | `:3080` | Listen address. |
| `YATA_SUWAYOMI_URL` | *(blank)* | Suwayomi-Server URL (`http://127.0.0.1:4567` in `:allinone`, `http://suwayomi:4567` with `--profile suwayomi`). Optional `YATA_SUWAYOMI_USER` / `YATA_SUWAYOMI_PASS`. |
| `YATA_TRANSLATE_PROVIDER` | *(blank)* | Server-side LLM translation provider (`openai` or `gemini`), with `YATA_TRANSLATE_ENDPOINT`, `YATA_TRANSLATE_API_KEY`, and `YATA_TRANSLATE_MODEL`. |
| `YATA_IMAGE_TRANSLATOR_URL` | *(blank)* | `manga-image-translator` URL (`http://manga-translator:8000` with `--profile translator`, or auto-wired on loopback when built with `WITH_IMAGE_TRANSLATOR=true`). Target language via `YATA_IMAGE_TRANSLATOR_LANG` (e.g. `PTB`, `ENG`). |

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

### ZimaOS / CasaOS

`zimaos.yml` is the same thing in the shape the App Store importer accepts: **App Store → + →
Install a customized app → Import**, paste the file. It differs from the compose above only in
mechanics — no `build:`, long-form ports and volumes, and data under `/DATA/AppData/yatagarasu` so
ZimaOS's own backups include it.

The image is public on GHCR, so nothing has to log in to pull it.

## Install the KOReader plugin

Open `http://<host>:3080/` in a browser and click **Download the KOReader plugin**. The ZIP is
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
GET    /api/health                    Karasu — "Test connection" + v2 capabilities
GET    /api/shelf                     Karasu + plugin
POST   /api/shelf                     Karasu — multipart metadata + file, upsert by chapterId
DELETE /api/shelf/{chapterId}         Karasu — 204 even when absent
GET    /api/shelf/{chapterId}/file    plugin — CBZ, supports Range
POST   /api/shelf/{chapterId}/read    plugin — idempotent, {"read":false} undoes it
POST   /api/sync/reconcile            Karasu — bidirectional library, categories, progress, merged sources & repos
GET    /api/chapters/locate           Karasu — check if a chapter CBZ exists locally or on Suwayomi
GET    /api/chapters/cbz              Karasu — stream CBZ over LAN (optional ?acquire=true via Suwayomi)
POST   /api/translations/translate    Karasu — server-side LLM translation + shared line cache
GET/PUT /api/translations/chapter     Karasu — shared per-chapter translation JSON
GET/PUT /api/translations/context     Karasu — shared per-series glossary/notes
GET/POST /api/backups                 Karasu — list & upload .tachibk backups
GET/DELETE /api/backups/{id}          Karasu — download or delete a backup
GET    /plugin.zip                    browser — the plugin, preconfigured
GET    /settings, POST /settings      browser — the settings page
POST   /api/shelf/{chapterId}/delete  browser — the remove button; forms cannot send DELETE
POST   /login, POST /logout           browser — trades the key for a session cookie
```

Programs authenticate with `Authorization: Bearer <key>`. Browsers POST the key once to `/login`
and carry a session cookie after that — the key is never accepted from a query string, so it stays
out of browser history and out of every proxy and tunnel log in the path.

The v1 shelf contract is specified in `docs/koreader-sync.md` and the v2 companion extensions in
`docs/yatagarasu-companion.md` in the [Karasu repo](https://github.com/clevim/Karasu).

## Storage

No database. One `<chapterId>.cbz` plus one `<chapterId>.json` per entry, under `/data`. The shelf
is bounded by Karasu's own limits (default 10 manga × 3 chapters), so listing it is a glob and a few
dozen small reads.

## Develop

```sh
go test ./...          # 21 tests, no dependencies
lua api_test.lua       # the plugin's download path; go test runs it too
go run .               # YATA_DATA=./data YATA_API_KEY= go run .
```

`TestChapterURLSurvivesRoundTrip` is the contract's enforcement: Karasu refuses to mark a chapter
read unless `chapterUrl` comes back byte for byte. Do not weaken it.

### Publishing

`.github/workflows/publish.yml` runs the `test` job first, then builds `linux/amd64` and
`linux/arm64` images and pushes both variants to `ghcr.io/clevim/yatagarasu`:

- **Lean `scratch` image** (`Dockerfile`): `:latest` on `master`, plus `:1.2.3` and `:1.2` on
  `v1.2.3` tags.
- **All-in-One image** (`Dockerfile.allinone`): `:allinone` on `master`, plus `:v1.2.3-allinone`
  and `:1.2-allinone` on `v1.2.3` tags.

```sh
git tag v1.0.0 && git push --tags
```

The package is private the first time it is created — flip it under *Package settings → Change
visibility → Public*, or the NAS needs a `docker login ghcr.io` to pull.

## License

MIT.
