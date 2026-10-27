<div align="center">

# 🛡️ Backupper

**Backups of servers and shared-hosting sites to Yandex Disk, Mail.ru Cloud, WebDAV and Telegram — on schedule, without your computer**

![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)
![Wails](https://img.shields.io/badge/Wails-v2-DF0000)
![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)
![Windows](https://img.shields.io/badge/panel-Windows%2011-0078D4?logo=windows&logoColor=white)
![Linux](https://img.shields.io/badge/agent-Linux%20amd64%20%7C%20arm64-FCC624?logo=linux&logoColor=black)
![PHP](https://img.shields.io/badge/shared%20hosting-PHP%207.1+-777BB4?logo=php&logoColor=white)

</div>

MySQL / MariaDB / PostgreSQL databases, site files and server configs are **streamed to the cloud in parts** — no temporary
archives on the server disk. Everything is configured from a Windows desktop panel, and the work happens on the server by itself.
Sites on shared hosting without SSH are backed up too, through a one-off PHP script uploaded over FTP / FTPS / SFTP.

The interface is available in **English and Russian**.

## ✨ Features

- **Independent of your PC.** A lightweight agent (systemd service) runs on each server and executes jobs on schedule.
- **No disk space needed.** `dump → gzip → encryption → parts in memory → cloud`. A 50 GB database backs up on a server with 2 GB free.
- **Smart part size.** Picked from free RAM, measured upload speed and cloud limits; during a backup parts shrink when memory runs low, and a part the network rejects is split in half.
- **Clouds:** Yandex Disk (REST API or WebDAV), Mail.ru Cloud (WebDAV), any WebDAV (Nextcloud, etc.) and **Telegram** through your own Bot API server (files up to 2 GB). Several clouds per job.
- **Sources:** MySQL/MariaDB (`mysqldump`), PostgreSQL (`pg_dump` / `pg_dumpall`), site files with excludes, server configs (~20 common paths).
- **CMS presets:** sites are found under server folders by their CMS, and caches, backups and other disposable folders are excluded automatically.
- **Shared hosting (FTP / FTPS / SFTP):** database and files of PHP sites in a separate **“Sites on hosting”** section; database access is read from the settings of 13 CMSs (1C-Bitrix, WordPress, Joomla, Drupal, OpenCart, MODX, PrestaShop, Magento, DLE, HostCMS, Webasyst, UMI.CMS, Laravel), manual settings are the fallback.
- **Schedule:** daily, on weekdays, monthly, every N hours or cron — with a preview of the next runs; a schedule can be paused with one switch.
- **Calendar rotation (GFS):** the N latest copies plus the newest per day, ISO week, month and year; failed and incomplete ones are removed. When the cloud has less free space than the previous copy, copies the policy would drop anyway are deleted before the upload.
- **Progress with an estimate:** percent and time left, from the exact tar size on a hosting or from the previous run.
- **Encryption** in the [age](https://age-encryption.org) format (works with `age -d`).
- **Telegram notifications** are sent by the agent itself — they arrive even when your PC is off.
- **Restore from the panel:** list backups straight from the cloud, download with decryption.
- **Secure:** the panel talks to the agent through an SSH tunnel with your key; the agent listens on `127.0.0.1` only and requires a token; panel secrets are encrypted with DPAPI.
- **Gentle on sites:** the agent runs with `nice 10` and low I/O priority.
- **One-click updates:** the panel notices outdated agents and updates them on one server or on all at once (servers busy with a backup wait).

## 🧭 How it works

```mermaid
flowchart LR
    subgraph PC["Windows — Backupper.exe panel"]
        UI["Interface<br/>React + Wails"]
    end
    subgraph S1["Your server"]
        A1["backupper-agent<br/>systemd, cron"]
        D1[("MySQL / PostgreSQL<br/>files, /etc")]
    end
    subgraph H["Shared hosting"]
        P["one-off PHP script"]
        W[("site DB<br/>and files")]
    end
    Y["☁️ Yandex Disk"]
    M["☁️ Mail.ru Cloud<br/>WebDAV"]
    T["✈️ Telegram<br/>own Bot API server"]
    UI -- "SSH tunnel with your key" --> A1
    D1 -- "dump | gzip | age" --> A1
    A1 -- "FTP: upload → HTTPS: fetch → delete" --> P
    W --> P
    A1 -- "parts of 8–512 MB" --> Y
    A1 -- "parts" --> M
    A1 -- "parts up to 2 GB" --> T
```

Backups are stored in the cloud like this:

```
<folder>/<server>/<job>/2026-10-23_03-00-00/
    shop__shop.sql.gz.age.part0001
    shop__shop.sql.gz.age.part0002
    site.tar.gz.age.part0001
    manifest.json
<folder>/sites/<site>/2026-10-23_04-00-00/...      # sites on shared hosting
```

## 🌐 Sites on shared hosting

For hostings with FTP but no SSH there is a separate menu section **“Sites on hosting”**. Each site is backed up by the agent
of a server you choose (“Through server”); moving a site to another server keeps its copies and rotation.

1. The agent connects over FTP / FTPS / SFTP and uploads `backupper_<random>.php` into the site root.
2. Over HTTPS it asks the script for the database dump and the files **in portions** of ~20 s each — this fits any `max_execution_time`, and nothing is written to the hosting disk.
3. Every request is signed with HMAC-SHA256 and a timestamp; every response is encrypted with AES-256-GCM using a secret that exists only for this run. Without a valid signature the script answers with a plain 404.
4. Right after the backup the script is deleted. Leftovers of a crashed run are removed on the next one.

Database credentials are read from the CMS settings files (`bitrix/.settings.php`, `wp-config.php`, `configuration.php`,
`.env` and others — 13 CMSs); the manual settings from the panel are used when those are missing or do not work. CMS caches
and backups are skipped. The **“Check hosting”** button uploads the script, shows PHP limits, the database and warnings, and deletes it.

Hosting quirks handled automatically: FTPS servers with broken TLS 1.3 session tickets (falls back to TLS 1.2),
certificates issued for another name (suggests the right address), JavaScript cookie bot protection such as Beget's.

## ✈️ Telegram as storage

Backups can go to your private Telegram channel through your **own** `telegram-bot-api --local` server — files up to 2 GB
and downloads without the 20 MB limit. The panel checks a server and installs the Bot API there over SSH (Docker or podman,
listening on `127.0.0.1` only); agents on other servers reach it through a restricted SSH user. Every copy is posted as
albums with hashtags `#server #job #dYYYYMMDD` and `manifest.json` last; a pinned message per server lists copies with links.
Jobs with Telegram storage must be encrypted.

## 🚀 Getting started

1. Download or build `Backupper.exe` and run it.
2. **Clouds** → connect Yandex Disk (OAuth token, instructions are in the form), Mail.ru Cloud (app password), WebDAV or Telegram.
3. **Add server** → address, user (`root` or a user with passwordless `sudo`), private SSH key → **Install agent**.
4. **New job** → sources, schedule, clouds, which copies to keep, encryption, part size.
5. **Sites on hosting** → **+** → FTP access, site address, server to work through → **Check hosting**.
6. **Settings** → encryption password, Telegram notifications, interface language.

## 🔧 Building

Requires Go 1.25+, Node.js 20+ and the Wails CLI:

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
.\build.ps1
```

The script builds agents for `linux/amd64` and `linux/arm64`, embeds them into the panel and produces `build\bin\Backupper.exe`.

Tests:

```powershell
go test ./internal/...
# end-to-end test of the shared-hosting backup with real PHP, MariaDB and SFTPGo (OSPanel modules)
$env:BACKUPPER_IT = "D:\OSPanel\modules"; go test ./internal/site -run IT -v
```

## ♻️ Restoring by hand

The panel is optional — parts are joined with standard tools:

```bash
cat site.tar.gz.age.part* | age -d | tar -xz -C /restore   # encrypted archive
cat shop__shop.sql.gz.part* | gunzip | mysql shop           # unencrypted dump
```

## 📁 Project layout

| Path | Contents |
|---|---|
| `cmd/agent` | agent entry point (`serve`, `install`, `uninstall`, `token`) |
| `internal/agent` | scheduler, sources, streaming upload, rotation, API |
| `internal/site` | shared-hosting backup: FTP/SFTP client, PHP script, signed protocol |
| `internal/cms` | CMS presets: site detection and disposable folders |
| `internal/chunk` | part size planner |
| `internal/storage` | Yandex Disk (REST), WebDAV and Telegram clients |
| `internal/desktop` | SSH, agent installation, Telegram Bot API server install, panel settings storage |
| `frontend` | panel interface (React + TypeScript), translations in `src/i18n.en.ts` |

## 📍 Where things live on the server

| | |
|---|---|
| Agent | `/usr/local/bin/backupper-agent`, service `backupper-agent` |
| Settings | `/etc/backupper/config.json` (`0600`, root) |
| Run history, SFTP host keys, Telegram index | `/var/lib/backupper/` |
| Service logs | `journalctl -u backupper-agent` |

## 👤 Author

Igor Gladyshev — Telegram [@gladyshev](https://t.me/gladyshev), [gladyshev18@gmail.com](mailto:gladyshev18@gmail.com)
