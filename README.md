# IDEAL

**ilbinek's Dedicated Environment for Arma on Linux**

A web-based Arma 3 **dedicated server manager for linux**: install server branches, run and stop server instances, download Workshop mods, import launcher modsets, and generate A3Sync repositories. Runs on Linux.

---

## Disclaimer

The frontend was built quickly for functionality over polish. Contributions that improve UI/UX are welcome.

---

## Features

- **Branches** — install Arma 3 dedicated server builds (built-in gosteam by default, or SteamCMD)
- **Servers** — create instances, assign mods, edit config/profiles, upload missions, start/stop/restart, live logs
- **Automation** — start servers when Ideal launches, restart them after unexpected exits, and restart them on a schedule with player warnings
- **Docker isolation (Linux)** — optional global setting to run each server in a local Docker container with host networking and Docker-enforced CPU/RAM limits
- **Mods** — download from Steam Workshop (built-in gosteam, DepotDownloader, or SteamCMD), update checks, download queue
- **Modsets** — import Arma 3 Launcher HTML presets
- **A3Sync** — generate a symlink-based repository from downloaded mods

---

## Screenshots

**Dashboard**

![Dashboard](screenshots/dashboard.png)

**Settings** — built-in gosteam downloader and Steam login

![Settings](screenshots/settings.png)

**Branches** — dedicated server installs

![Branches](screenshots/branches.png)

**Modsets** — import Arma 3 Launcher HTML presets

![Modsets](screenshots/modsets.png)

**Mods** — Workshop downloads, updates, and A3Sync generation

![Mods](screenshots/mods.png)

**Servers** — instance list

![Servers](screenshots/servers.png)

**Server overview** — branch, ports, launch params, command preview

![Server overview](screenshots/server-overview.png)

**Server mods** — Workshop / custom mods and provisioning

![Server mods](screenshots/server-mods.png)

**Server mods (local folders)** — scan a mods root or add custom paths

![Server mods local](screenshots/server-mods-local.png)

**Keys** — automatic `.bikey` collection

![Keys](screenshots/server-keys.png)

**Config** — simple / raw editors for `server.cfg`, `basic.cfg`, and profile

![Config](screenshots/server-config.png)

**Headless clients**

![Headless clients](screenshots/headless-clients.png)

**Missions** — upload and optional HTTP serving

![Missions](screenshots/missions.png)

---

## Prerequisites

### Downloader (pick one in Settings)

Used for **Workshop mods** and **dedicated server branch installs** (app `233780`).

| Downloader | Notes |
|---|---|
| **Built-in (gosteam)** — default | No external binary. Log in via Settings (credentials or QR). Supports Steam Guard. Session token stored under `.gosteam/`. Covers mods and server branches — SteamCMD is not required. |
| **DepotDownloader** | [SteamRE/DepotDownloader](https://github.com/SteamRE/DepotDownloader) on `PATH` or an explicit path in Settings. Per-file progress for mods. Branch installs still use SteamCMD when this option is selected. |
| **SteamCMD** | Optional. Coarse progress; workshop items are moved into `./mods/<id>` after download. Also used for branch installs when selected. |

For DepotDownloader / SteamCMD mod downloads, Steam username and password are stored in the SQLite DB (plain text). Prefer a dedicated Steam account. The built-in downloader does **not** require storing a password long-term once logged in.

A Steam Web API key (Settings) is used for update checks.

### Docker isolation (Linux only)

Optional. In **Settings → Server isolation**, enable **Use Docker for servers**. Ideal must be able to reach a local Docker Engine (`docker` on `PATH`, daemon running).

When enabled, each dedicated server starts as container `ideal-server-<id>` with:

- **Host networking** (game/Steam ports and headless `127.0.0.1` unchanged)
- **Bind-mount** of the Ideal data root at the same absolute path (so instance symlinks resolve); custom mod folders outside the data root are mounted read-only as needed
- **CPU affinity** via `--cpuset-cpus`
- **RAM limit** via `--memory=Nm` and `--memory-swap=2Nm` (allows swap equal to the RAM cap)

Headless clients run inside the same container (`docker exec`) so they share that memory/CPU budget. Windows installs keep process-based launch (Job Objects / affinity) and ignore this setting.

On first use Ideal builds the `ideal-arma-runtime:local` image from the embedded Dockerfile (same as `docker/arma-runtime/Dockerfile`). You can also build it manually:

```bash
docker build -t ideal-arma-runtime:local docker/arma-runtime
```

The mode applies on the **next** server start (running processes/containers are not migrated mid-flight).

---

## Installation (release)

1. Download the release for your platform and extract it
2. Edit `config.json` and change `password` from `changeme` (Ideal **refuses to start** until you do)
3. Run the binary:
   ```bash
   # Linux
   ./ideal
   ```
4. Open `http://localhost:8080` (or the port in `config.json`)
5. Log in with the password you set, or sign in through Steam (Steam users must be added first on the Users page)

---

## Configuration

### `config.json`

```json
{
  "password": "changeme",
  "port": "8080"
}
```

| Field | Purpose |
|---|---|
| `password` | Bootstrap admin password (password login). Always has full access. |
| `port` | HTTP listen port (unless overridden by `IDEAL_LISTEN`). |

Steam OpenID login is always available. Behind a reverse proxy, set `X-Forwarded-Proto` and `X-Forwarded-Host` (or Host) so Steam callback URLs match the browser URL.

Steam credentials, downloader choice, and related options live in the SQLite database and are edited in the **Settings** UI — not in `config.json`.

### Users and permissions

- **Password admin** — logs in with `config.json` `password`; always full admin.
- **Steam users** — must be added manually on the **Users** page (SteamID64 or steamcommunity.com profile/vanity URL) before they can sign in. Grant roles there as needed.
- **Roles:** full admin; manage mods; manage branches; per-server admin (assigned servers only).
- Sessions are stored in SQLite and last 30 days (survive restarts).

### Environment variables

| Variable | Purpose |
|---|---|
| `IDEAL_LISTEN` | Override HTTP bind address (e.g. `127.0.0.1:8080`). Default is all interfaces on the configured port (`:8080`). A loopback address also keeps server port checks on loopback, which avoids firewall prompts during local testing, but those checks can then miss a port held on another interface. |
| `IDEAL_DATA_DIR` | Override the data root (config, DB, mods, servers, branches). |

---

## Usage

### 1. Install a server branch

1. Open **Branches**
2. Create a branch (name + optional Steam beta branch)
3. Install it — Ideal downloads the dedicated server (app `233780`) with the configured downloader (gosteam by default)

### 2. Create and run a server

1. Open **Servers** → create a server (pick a branch, ports, optional modset)
2. On the server detail page, assign client/server mods, edit config, upload missions as needed
3. Click **Start** — Ideal provisions the instance (`servers/<slug>/`), copies keys, and launches the process (or a Docker container when Server isolation is enabled on Linux)
4. Use **Stop** / **Restart** from the UI; live status updates over SSE. Both shut down gracefully (see [Automation](#automation)); **Force stop** appears while a server is stopping and kills it right away
5. **Logs** tab tails session output and RPT files

Default ports per server: game `2302`, Steam query `2303` (Steam defaults to game port + 1 if omitted).

**Duplicate** (server header, full admins) makes an independent copy under a new name — e.g. to test new addons without touching the live setup. It copies all settings (automation toggles start switched off, though the schedule is kept, so a copy doesn't start or restart itself), the mod list (incl. server-only / optional flags), custom keys, server admins, headless clients, `server.cfg`/`basic.cfg`, profiles and missions. Ports are shifted by the next free multiple of 100 (e.g. `2302` → `2402`, BattlEye `RConPort` likewise) and the copy's `server.cfg` hostname is set to the new name. Logs, crash dumps, caches and generated folders (`mods/`, `keys/`, …) are not copied; they are rebuilt on provision.

After Ideal restarts, transitional statuses are reconciled (e.g. a stuck `stopping` after a reboot becomes `stopped` when the process is gone). Servers still running are picked back up, once Ideal has checked that the recorded process really is that server and not something else that reused its PID.

### Automation

Each server's **Automation** tab has three independent settings. Server admins can change them; a full admin can **Pause all automation** in Settings for maintenance, which also cancels anything pending, with no catch-up afterwards.

| Setting | What it does |
|---------|--------------|
| **Start when Ideal launches** | Starts the server whenever Ideal starts, one server at a time after a short delay (both set in Settings). This is what brings a server back after a power loss, provided Ideal itself gets started. |
| **Restart if it exits unexpectedly** | Restarts after any exit not requested through Ideal, including `#shutdown` typed in-game. Waits 10s, 30s, 1m, then 5m between tries, and stops trying after 5 failures in 15 minutes. With this off, Ideal never restarts the server by itself. |
| **Restart on a schedule** | Restarts at set times and weekdays. Never starts a stopped server, and skips one started under 10 minutes earlier or a time Ideal wasn't running for. Times are entered in your browser's timezone and stored in UTC, so they move by an hour locally when daylight saving starts or ends. |

**Stopping a server on purpose keeps it off**, including across Ideal restarts, until someone starts it again. The same goes for a server Ideal gave up on.

Automatic starts use the newest downloaded mods and branch files, just like a manual restart, and the server's **History** notes when that changed anything. History also records starts, stops, exits and skipped restarts. If a Discord webhook is set, you're told about unexpected exits, giving up, and failed launch or planned starts.

Ideal only notices a server has gone when its process exits, so a frozen server isn't restarted.

**Graceful shutdown.** Stop, Restart and planned restarts try each of these in turn, moving on if the server is still up:

1. **BattlEye RCON** `#shutdown`, waiting up to 45s. This needs BattlEye on and RCON set up: the Automation tab reads the password and port from the server's BattlEye config, and **Generate RCON settings** adds any that are missing without changing existing values. Generated settings take effect the next time the server starts. RCON is also what lets players be warned before a planned restart, using the offsets you set; without it they get no warning.
2. **Asking the OS to close it**, waiting up to 30s: closing its window on Windows, or a signal on Linux and Docker.
3. **A hard kill**.

### 3. Mods and modsets

**Modsets:** export an HTML preset from the Arma 3 Launcher → **Modsets** → upload. Mods are added to the database.

**Mods page:**
- Download individual mods or enqueue missing/outdated ones
- Check for updates (needs Steam API key)
- Add a mod by Workshop ID
- Delete mods to free disk space

Downloaded mods live under `./mods/<workshopId>/`. An `./addons/` tree of lowercased symlinks is maintained for server provisioning.

### 4. Keys

For **managed servers**, `.bikey` files are copied into `servers/<slug>/keys/` automatically when the instance is provisioned. You do not need to copy keys by hand for Ideal-started servers.

**Optional / client-side mods:** in a server's **Mods** tab set a mod's **Load as** to **Optional (key only)**. Its `.bikey` is added to the server's keys, but the mod is not passed to `-mod=`/`-serverMod=` (nor to headless clients), so players may run it optionally.

**Signature check:** a shield icon next to each mod (Mods page and server mod list) shows signing health — green: every `addons/*.pbo` has a `.bisign` and a matching `.bikey` exists; yellow: PBOs are signed but the matching `.bikey` is missing; red: some PBOs have no `.bisign`. Hover for details. In the server list, keys already present in the server's `keys/` (custom uploads, other mods) count as available. Bisigns are matched to bikeys by their embedded public key, not by file name; PBO contents are not re-hashed.

### 5. A3Sync folder

1. Open **Mods**
2. Click **Generate A3Sync Folder**
3. Ideal creates `./a3sync` with file-level symlinks to downloaded mods

Point your A3Sync repository at that directory.

---

## Directory structure

```
.
├── ideal / ideal.exe     # Main executable (release) or under dist/ when built with Task
├── config.json           # Admin password and port
├── arma3mods.db          # SQLite database
├── .gosteam/             # Built-in downloader token/cache
├── mods/                 # Workshop mods (<workshopId>/)
├── addons/               # Lowercased symlink mirrors for provisioning
├── keys/                 # Keys for legacy/manual startup-command flows
├── a3sync/               # Generated A3Sync repository
├── arma3/branches/       # Installed dedicated server branches
├── servers/<slug>/       # Per-server instances (configs, mods, logs, missions)
├── docker/arma-runtime/  # Dockerfile for the Linux Arma runtime image
├── web/                  # React SPA source
├── web/dist/             # Built SPA (embedded via go:embed; not committed)
├── screenshots/          # UI screenshots for this README
└── dist/                 # task build / task run output
```

The SPA is embedded in the binary with `go:embed`, so releases do not ship a separate `web/dist` folder.

---

## Development

Requires [Task](https://taskfile.dev), Go (see `go.mod`), Node.js/npm, and a C compiler (**CGO** is required for SQLite).

```bash
git clone https://gitea.tbdevent.eu/ilbinek/ideal.git
cd ideal
task install
```

### Useful tasks

| Task | Description |
|---|---|
| `task install` | Download Go modules and install frontend deps |
| `task dev` | Run the Go API with `IDEAL_LISTEN=127.0.0.1:8080` |
| `task web:dev` | Vite dev server (proxies `/api` → `:8080`) |
| `task web:build` | Build the SPA into `web/dist` |
| `task build` | Build SPA (if needed) + binary → `dist/ideal` |
| `task run` | Build if sources changed, then run `dist/ideal` |
| `task build:all` | Cross-compile Linux amd64/arm64 into `dist/` |
| `task lint` | `go vet` + frontend typecheck |
| `task clean` | Remove `web/dist` and `dist` |

### Dev workflow (hot-reload UI)

```bash
# Terminal 1 — API
task dev

# Terminal 2 — frontend
task web:dev
```

Open `http://localhost:5173` (Vite). The API listens on `127.0.0.1:8080`.

### Production-like local run

```bash
# Edit config.json password first
task run
```

Opens Ideal with the embedded UI on the port from `config.json` (all interfaces unless `IDEAL_LISTEN` is set).

Do **not** use bare `go build` without building the SPA first — the binary embeds `web/dist`, which is produced by `task web:build` / `task build`.

---

## Troubleshooting

**"Default password is still in use"**  
Change `password` in `config.json` away from `changeme`.

**Downloader / Steam login issues**  
- Built-in: complete Steam login in Settings (Guard/QR supported)  
- DepotDownloader / SteamCMD: ensure the binary is on `PATH` or set the path in Settings  

**Branch install fails**  
- Built-in: log in to Steam from Settings; the account must own / be entitled to the dedicated server  
- SteamCMD / DepotDownloader: SteamCMD must be on `PATH` (or set in Settings); username/password in Settings must be valid

**Server stuck or `unknown` after reboot**  
Ideal reconciles statuses on startup. If a process is gone, status should settle to `stopped` / `unknown`. Use Stop/Start from the UI if needed. If Ideal couldn't verify that a live process belongs to the server, it leaves that process alone and says so in the server's History; Stop then reports this rather than killing it.

**Game port not available**
A server only counts as running if its own process holds the port. The Start error names whatever is holding it, so you can close it or change the server's ports.

**A server came back on its own, or won't**
Check the server's **History** on its Automation tab, which records what Ideal did and why. To keep a server down, use **Stop** rather than closing it another way, as anything else counts as an unexpected exit.

**Empty or broken UI after `go build`**  
Build the frontend first (`task web:build` or `task build`).

**CGO / sqlite build errors**  
Install a C toolchain (e.g. gcc) and ensure `CGO_ENABLED=1`.
