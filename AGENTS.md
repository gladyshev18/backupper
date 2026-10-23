# AGENTS.md — working on Backupper

Guide for AI coding agents (Claude Code, Codex, …). Read it before changing anything.
The owner speaks Russian: answer in Russian, keep code comments in English.

## What this is

Backup system for servers and shared-hosting sites → Yandex Disk / Mail.ru Cloud / WebDAV.

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
| `internal/desktop` | panel config (`%APPDATA%\Backupper\panel.dat`, DPAPI-encrypted), SSH dial/keys/host-key pinning, tunnel `Manager`, agent install/adopt/uninstall |
| `internal/agent` | agent: `state.go` (config, history, speed stats), `agent.go` (cron + single worker queue), `runner.go` (run, manifest, rotation), `pipeline.go` (part uploader, retries, adaptive shrink/split), `sources.go` (mysqldump/pg_dump/tar commands), `site_source.go` (hosting sources, SFTP known_hosts), `api.go`, `install.go` (systemd), `notify.go` (Telegram) |
| `internal/site` | `script.php` (embedded, PHP 7.1+), `site.go` (session, HMAC requests, AES-GCM responses, retries), `remote.go` (FTP/FTPS/SFTP) |
| `internal/chunk` | part-size planner + explanations (RU, EN in `messages_en.go`) |
| `internal/storage` | Yandex Disk REST + generic WebDAV (Mail.ru, Yandex WebDAV, Nextcloud) |
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
Rotation keeps the newest N folders whose manifest says success/warning.

## Status / not yet verified

- Not tested against real Yandex Disk, Mail.ru Cloud, a real VPS install, or real shared hostings
  (protocols are covered by local tests with WebDAV, PHP, MariaDB, SFTPGo).
- Mail.ru WebDAV availability depends on the plan.

## Git

Remote: `https://github.com/gladyshev18/backupper` (public), branch `main`, `gh` active account `gladyshev18`.
Commit only when the owner asks; commit messages in Russian.
