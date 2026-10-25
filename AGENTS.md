# AGENTS.md — working on Backupper

Guide for AI coding agents (Claude Code, Codex, …). Read it before changing anything.
The owner speaks Russian: answer in Russian, keep code comments in English.

## What this is

Backup system for servers and shared-hosting sites → Yandex Disk / Mail.ru Cloud / WebDAV / Telegram (own Bot API server).

- **Agent** (`cmd/agent`, Linux, systemd) — keeps jobs and the cron schedule, dumps sources and **streams** them
  (gzip → optional age encryption → parts held in RAM → cloud). Nothing is written to the server disk
  unless the "disk buffer" mode is chosen. HTTP API on `127.0.0.1:7171`, bearer token.
- **Panel** (repo root, Wails v2: Go + React/TS, Windows) — manages agents through an **SSH tunnel**
  (the agent port is never exposed), installs the agent over SSH, edits jobs, shows history, downloads backups.
- **Shared hosting** (`internal/site`) — the agent uploads a one-off PHP script over FTP/FTPS/SFTP, pulls the DB dump
  and a tar of the files in signed + encrypted portions over HTTP(S), then deletes the script.

The panel owns global settings (clouds, encryption password, Telegram, language) and pushes them to every agent
(`PUT /api/sync`) on first call of a session and after edits. Jobs live on the agent (`/etc/backupper/config.json`).

## Layout

| Path | Role |
|---|---|
| `main.go`, `app.go` | Wails entry; `App` methods are bound to JS (`frontend/wailsjs` is generated) |
| `internal/desktop` | panel config (`%APPDATA%\Backupper\panel.dat`, DPAPI-encrypted), SSH dial/keys/host-key pinning, tunnel `Manager`, agent install/adopt/uninstall, `tgserver.go` (check + install of the Telegram Bot API server) |
| `internal/agent` | agent: `state.go` (config, history, speed stats), `agent.go` (cron + single worker queue), `runner.go` (run, manifest, rotation), `pipeline.go` (part uploader, retries, adaptive shrink/split), `sources.go` (mysqldump/pg_dump/tar commands), `site_source.go` (hosting sources, SFTP known_hosts), `api.go`, `install.go` (systemd), `notify.go` (Telegram) |
| `internal/site` | `script.php` (embedded, PHP 7.1+), `site.go` (session, HMAC requests, AES-GCM responses, retries), `remote.go` (FTP/FTPS/SFTP) |
| `internal/cms` | CMS presets (markers + disposable folders): site search under server folders → tar excludes; the same list goes to `script.php` |
| `internal/chunk` | part-size planner + explanations (RU, EN in `messages_en.go`) |
| `internal/storage` | Yandex Disk REST + generic WebDAV (Mail.ru, Yandex WebDAV, Nextcloud) + Telegram (`telegram.go`, `telegram_transport.go`; fake Bot API for tests in `telegramtest`) |
| `internal/model` | types shared by panel and agent (JSON contract between them) |
| `internal/version` | single version for panel and agent — **bump it** when the agent changes; the panel then offers "Update agent" |
| `agentbin` | embeds `dist/backupper-agent-linux-{amd64,arm64}` (built by `build.ps1`, git-ignored) |
| `frontend/src` | React UI: `App.tsx` (layout, routing, app context), `pages/*`, `components/ui.tsx`, `api.ts` (typed wrapper over bindings), `types.ts` (mirror of Go JSON), `i18n.ts` + `i18n.en.ts` |

## Commands (Windows; Go is in `C:\Program Files\Go\bin`, Wails CLI in `%USERPROFILE%\go\bin`)

```powershell
.\build.ps1                         # agents (linux amd64/arm64) + panel → build\bin\Backupper.exe
go vet ./... ; go test ./internal/...
cd frontend; npx tsc --noEmit        # type-check the UI
wails generate module                # regenerate frontend/wailsjs after changing App methods
```

- `build.ps1` deliberately uses `$ErrorActionPreference = "Continue"`: wails writes progress to stderr.
- Agent code must stay buildable on Windows (tests run there): Linux-only bits go behind build tags (`disk_unix.go`).
- Linux-only test (`execute_linux_test.go`, real GNU tar): `GOOS=linux go test -c ./internal/agent` and run the binary in WSL.
- Hosting end-to-end test with real PHP 7.4/8.3 + MariaDB + SFTPGo from OSPanel:
  `$env:BACKUPPER_IT="D:\OSPanel\modules"; go test ./internal/site -run IT -v`.
  MariaDB there runs with `--no-defaults` in a temp dir — **never** start `mysqld` without it: OSPanel's global
  config points at the user's real data in `D:\OSPanel\data`.

## UI testing without servers

`npm --prefix frontend run dev` (Vite, port 5173; `.claude/launch.json` has it) and inject a fake `window.go.main.App`
object returning sample data, then re-import `/src/main.tsx`. Never test against the real panel config.

## Conventions

- **Interface text**: always `t('Русская фраза', { vars })` from `src/i18n.ts`; the Russian phrase is the key and the
  English goes into `src/i18n.en.ts`. Labels from maps (`sourceLabels`, `kindLabels`, `statusLabels`) stay Russian
  and are wrapped with `t()` where shown. Missing keys fall back to Russian.
- Agent logs, errors and Telegram messages are Russian (not translated yet); chunk-plan explanations are bilingual
  (`?lang=` on `/api/plan`).
- Go JSON fields are camelCase; when adding a field update `internal/model`, `frontend/src/types.ts` and both sides.
- New agent API → handler in `internal/agent/api.go`, wrapper in `app.go`, typed call in `frontend/src/api.ts`.
- Secrets never go into logs, URLs or the repo. Test credentials only in tests.
- Keep the code style of the surrounding file; small focused comments explain *why*.

## Agent API (all `Authorization: Bearer <token>`)

`GET /api/info` · `GET /api/sysinfo` · `PUT /api/sync` · `PUT|DELETE /api/jobs/{id}` (`new` creates) ·
`POST /api/jobs/{id}/run` · `POST /api/plan?lang=` · `GET /api/runs?job=&limit=` · `GET /api/runs/{id}` ·
`POST /api/runs/{id}/cancel` · `GET /api/active` · `POST /api/storages/test` · `GET|DELETE /api/backups` ·
`GET /api/download?storage=&dir=&file=&decrypt=1` · `POST /api/probe` · `POST /api/telegram/test`

## Hosting script protocol (`internal/site`)

POST form `a` (action: info|db|files|cleanup), `p` (JSON params incl. opaque `state`), `t` (unix time, corrected by the
hosting `Date` header), `n` (nonce), `s` = hex HMAC-SHA256(`sha256("mac"+secret)`, `a|t|n|p`). Response body =
`iv(12) | AES-256-GCM(sha256("enc"+secret), 4-byte meta length | meta JSON | data) | tag(16)`; each `data` is one gzip
member, so concatenated responses form a valid `.gz`. Bad signature → plain 404. Script name `backupper_<16 hex>.php`;
stale ones matching that exact pattern are deleted on the next run.

## Cloud layout

`<root>/<server slug>/<job slug>/<YYYY-MM-DD_HH-MM-SS>/<file>.partNNNN` + `manifest.json` (status, files, parts, sha256).
Rotation follows `Job.Retention` (GFS: last N + newest copy per day/ISO week/month/year, `model.Retention.Select`);
an empty retention falls back to `keep` (jobs from agents ≤ 1.1). Only folders whose manifest says success/warning count;
older broken ones are removed. A file that fails mid-upload deletes its parts at once; when the cloud has less free space
than the previous copy, copies the policy would drop anyway are removed before the upload.

## Telegram storage (`model.StorageTelegram`)

Files go to a Telegram chat (the owner's private channel) through an **own** `telegram-bot-api --local` server: uploads
up to 2000 MB (`storage.TelegramMaxFile`), downloads without the 20 MB limit. Jobs with a Telegram storage must be
encrypted (agent refuses others: `needsEncryption`).

- **Server** (panel → Clouds → Telegram, `desktop/tgserver.go`): read-only check (OS, sudo, systemd, RAM/disk,
  api.telegram.org TLS time, download speed from telegram.org), then install over admin SSH: Docker (`docker.io`) or podman,
  image `aiogram/telegram-bot-api` run as the relay user with `--entrypoint /usr/local/bin/telegram-bot-api --local`,
  systemd `backupper-tg-bot-api` publishing `127.0.0.1:8081` only, data in `/var/lib/backupper-tg`, hourly
  `backupper-tg-clean.timer` for forgotten downloads, `logOut` from the cloud Bot API (bot of notifications is refused).
  api_id/api_hash go to `/etc/backupper-tg/env` (0600), never to the command line.
- **Delivery to agents on other servers**: SSH as `backupper-tg` (`model.TelegramRelayUser`) with an ed25519 key made by
  the panel; `authorized_keys` has `restrict,port-forwarding,permitopen="127.0.0.1:8081",command="internal-sftp"`.
  Agents forward HTTP to the Bot API and read + delete `getFile` results (absolute paths on that disk) over SFTP. The key
  and pinned host key travel in `Storage.TgServer`; admin fields and api_hash are stripped by `Storage.ForAgent()` on sync.
  `TgServer.DirectURL` skips SSH (tests, Bot API on the same host).
- **Layout in the chat**: each part goes up as a silent "⏳ uploading" message; `Put(manifest.json)` re-sends the parts by
  file_id as albums of 2–10 captioned `#server #job #dYYYYMMDD` + `file — часть N из M`, deletes the temporary
  messages and posts `manifest.json` last (with `Manifest.Telegram`: part → message_id/file_id). No manifest message =
  incomplete copy. A pinned message per server lists copies with links (edited, recreated if gone).
- **Index**: the Bot API cannot read history, so the folder tree lives in `<agent data>/telegram-<storageID>.json`
  (`storage.SetDataDir`, set by `OpenState`). Losing it leaves copies in the chat (manifests restore by hand) but the
  agent no longer lists them.
- **Rotation / 48 hours**: a service message is sent with the first part; 49 h later the agent deletes it and checks the
  result (`editMessageReplyMarkup` → "not found" = deleted). If old messages cannot be deleted, `RotationBlocked()` turns
  rotation off for that storage, `/api/info` reports it in `storageNotes`, the job editor shows it.
- Flood control: `429` waits `retry_after` (up to 6 times). Errors never contain the bot token.

## Status / not yet verified

- Not tested against real Yandex Disk, Mail.ru Cloud, a real VPS install, or real shared hostings
  (protocols are covered by local tests with WebDAV, PHP, MariaDB, SFTPGo).
- Mail.ru WebDAV availability depends on the plan.
- Telegram: covered by the fake Bot API only; not yet run against a real bot/channel/Bot API server (48-hour deletion,
  hashtag linking, albums of documents, docker image entrypoint path, speed from the owner's VPS).

## Git

Remote: `https://github.com/gladyshev18/backupper` (public), branch `main`, `gh` active account `gladyshev18`.
Commit only when the owner asks; commit messages in Russian.
