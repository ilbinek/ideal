# IDEAL
## ilbinek's Dedicated Environment for Arma Linux

A web-based Arma 3 mod manager for downloading, organizing, and managing mods from Steam Workshop using DepotDownloader. Supports modsets, automatic key extraction, and A3Sync folder generation.

---

## ⚠️ Disclaimer

The frontend is vibecoded (quickly built for functionality over perfection). Contributions to improve the UI/UX are welcome!

---

## Prerequisites

### Required Tools

1. **A downloader** - one of the following, selectable in Settings:
   - **DepotDownloader** (default) - [SteamRE/DepotDownloader](https://github.com/SteamRE/DepotDownloader).
     Reports per-file download progress.
   - **SteamCMD** - [SteamCMD docs](https://developer.valvesoftware.com/wiki/SteamCMD).
     Progress is coarse (stage messages, then 100%).
   - Each must be on your system `PATH`, or you can set an explicit path to the
     executable in **Settings → Downloader** (blank = use `PATH`).
   - The application will fail to download mods if the selected downloader cannot
     be found.

2. **Steam Account Credentials**
   - You'll need Steam credentials configured in settings
   - The account must have Steam Guard disabled
   - It's recomeneded to use a dedicated Steam account for this automation, credentials are saved as plain text in the DB

---

## Installation

1. Download the appropriate release for your platform
2. Extract the archive
3. Run the executable:
   ```bash
   # Linux
   ./ideal
   
   # Windows
   ideal.exe
   ```
4. Access the web interface at `http://localhost:8080` (default port)
5. Login with defined password (configurable in config.json)

---

## Configuration

### First Time Setup

1. Navigate to **Settings** page
2. Configure your Steam credentials:
   - **Username**: Your Steam username
   - **Password**: Your Steam password
   - **API Key**: Steam Web API key for update checking

---

## Usage Guide

### 1. Uploading Modsets

Modsets are HTML presets exported from the Arma 3 Launcher.

**Steps:**
1. In Arma 3 Launcher, create a preset with your desired mods
2. Click **Export** and save the HTML file
3. In IDEAL, go to **Modsets** page
4. Click **Upload New Modset**
5. Select the `.html` preset file
6. The modset will be parsed and all mods will be added to the database

### 2. Managing Mods

**Download Mods:**
- Navigate to the **Mods** page
- Click **Download** next to any mod to fetch it from Steam Workshop
- Real-time progress tracking shows download status
- Downloaded mods are stored in the `./mods` directory

**Check for Updates:**
- Click **Check Updates** to scan all mods for newer versions
- Mods with available updates will be highlighted
- Re-download to update to the latest version

**Delete Mods:**
- Click **Delete** to remove a mod and free up disk space
- This removes the mod files and database entry

### 3. Server Startup Configuration

**Generate Startup Parameters:**
1. Go to **Home** page or click a modset
2. Click **Generate Startup Command**
3. You'll see:
   - **Mod Parameter**: The `-mod=` parameter line for your Arma 3 server
   - **Keys Path**: Location of extracted `.bikey` files
   - **Full Example Command**: Complete server start command

**Copy the mod parameter line and use it in your server startup script.**

### 4. Managing Keys

When you generate the startup command, IDEAL automatically:
1. Scans all downloaded mods for `.bikey` files
2. Copies them to the `./keys` directory
3. Displays the path in the startup command page

**Important:** 
- After copying the mod parameter, **manually copy all `.bikey` files** from the displayed keys directory to your Arma 3 server's `keys/` folder
- This is required for the server to verify mod signatures
- Keys must match the mods in your mod parameter

### 5. A3Sync Folder Generation
#### Linux only

A3Sync is a repository system for distributing Arma 3 mods to players.

**Generate A3Sync Structure:**
1. Go to **Mods** page
2. Click **Generate A3Sync Folder**
3. IDEAL creates an `./a3sync` directory with:
   - File-level symlinks to all mod files (space-efficient)
   - Proper folder structure for A3Sync repository
   - All downloaded mods included automatically

**Using the A3Sync Folder:**
- Point your A3Sync server configuration to the `./a3sync` directory
- Players can sync mods directly from your repository
- Symlinks mean no duplicate files (saves disk space)
- Updates to mods in `./mods` are reflected in `./a3sync`

---

## Directory Structure

```
.
├── ideal (or ideal.exe)       # Main executable
├── config.json                # Application configuration
├── mods/                      # Downloaded mods (Workshop ID folders)
│   ├── 450814997/            # Example: CBA_A3
│   └── 463939057/            # Example: ACE3
├── keys/                      # Extracted .bikey files
├── a3sync/                    # A3Sync repository (generated)
└── database.db               # SQLite database
```

The web UI (a React single-page app) is compiled into `web/dist` and embedded
directly in the binary via `go:embed`, so no separate asset folders need to ship
alongside the executable. Frontend source lives in `web/` and is built with
[Task](https://taskfile.dev) (`task build` builds the SPA and the binary; see
the Taskfile for `dev`/`web:dev`/`lint` targets).


---

## Troubleshooting

**"DepotDownloader not found" / "steamcmd download failed"**
- Ensure the selected downloader is installed and on your system PATH, or set an
  explicit path in **Settings → Downloader**
- Test by running `DepotDownloader` (or `steamcmd`) in a terminal
- SteamCMD writes items to `<steamcmd>/steamapps/workshop/content/107410/<id>`;
  the app moves them into `./mods/<id>` automatically after a successful run

**Download fails with authentication error**
- Verify Steam credentials in Settings
- Some accounts may require Steam Guard approval
- Check if your account owns Arma 3

**Symlink creation fails (A3Sync)**
- On Windows, you may need Administrator privileges
- On Linux, ensure proper file permissions

**Mods not appearing in startup command**
- Ensure mods are fully downloaded
- Check that mod directories exist in `./mods`
- Refresh the page after downloads complete

---

## Building from Source

```bash
# Clone the repository
git clone <repository-url>
cd modmanager

# Build
go build -o ideal .

# Run
./ideal
```

---

## Contributing

Issues and pull requests are welcome, especially for frontend improvements!

---
