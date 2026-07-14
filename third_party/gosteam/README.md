# gosteam

A Go library and CLI for downloading Steam Workshop mods and full Steam
applications (server files, game depots, beta branches) over a **single login**,
with **delta patching** (only changed chunks are re-downloaded) and **pruning**
of files removed on the remote.

It ports the orchestration logic of [DepotDownloader] onto Steam-network
plumbing from a vendored fork of [0xAozora/go-steam], extended with the pieces it
was missing: zstd chunk support, CDN auth tokens, QR login, workshop →
depot/manifest resolution, and PICS-based app/branch resolution (including
password-protected betas).

[DepotDownloader]: https://github.com/SteamRE/DepotDownloader
[0xAozora/go-steam]: https://github.com/0xAozora/go-steam

## Building

With [go-task](https://taskfile.dev):

```sh
task build       # current platform -> bin/gosteam[.exe]
task build-all   # cross-compile -> dist/gosteam_{windows,linux}_{amd64,arm64}[.exe]
task test        # run tests
task run -- download 450814997   # build and run
```

Or plain Go: `go build -o gosteam ./cmd/gosteam`. The project is pure Go
(`CGO_ENABLED=0`), so cross-compilation needs no toolchain setup.

## CLI

```sh
go build -o gosteam ./cmd/gosteam

# Establish + cache a refresh token (password only needed the first time):
./gosteam login --user YOURNAME

# Download some Arma 3 workshop mods (or a collection id) concurrently:
./gosteam download --user YOURNAME --out ./mods 450814997 463939057

# Headless / no password typing:
./gosteam download --qr --out ./mods 450814997

# Re-run later: unchanged files are skipped, only changed chunks fetched,
# removed files pruned.
./gosteam download --user YOURNAME --out ./mods 450814997 463939057

# Verify and repair an existing install:
./gosteam download --user YOURNAME --out ./mods --verify-all 450814997

# Download an application (e.g. Arma 3 Dedicated Server) for a branch:
./gosteam app-download --user YOURNAME --app 233780 --branch public --out ./server

# Named / beta branch (password-protected betas need --branch-password):
./gosteam app-download --user YOURNAME --app 233780 --branch profiling --out ./server-perf
./gosteam app-download --user YOURNAME --app 233780 --branch secretbeta \
  --branch-password 'thepassword' --out ./server-beta

# Show what's installed:
./gosteam list
```

Workshop items land in `mods/<publishedfileid>/`. Application depots are merged
into `--out` (SteamCMD-style flat install). State (refresh tokens, installed
manifest maps, and the manifest cache used for diffing) lives under `.gosteam/`
(configurable with `--state`).

Workshop flags: `--app` (default 107410, Arma 3), `--max-downloads`,
`--max-mods`, `--verify-all`, `--no-prune`, and auth selectors
`--user`/`--password`, `--qr`, `--anonymous`.

App-download flags: `--app` (required), `--branch` (default `public`),
`--branch-password`, `--out`, `--os` / `--arch` / `--language`,
`--all-platforms` / `--all-archs` / `--all-languages`, `--depot` / `--manifest`
(comma-separated overrides), `--max-downloads`, `--max-depots`, `--verify-all`,
`--no-prune`.

> Most content is ownership-gated, so anonymous login generally will not work —
> use an account that owns (or is entitled to) the target app.

## Library

The CLI is a thin shell over composable packages:

| Package | Responsibility |
|---|---|
| `pkg/client` | Connect + authenticate (anonymous / refresh-token / credentials+Guard / QR); returns a live `Session`. |
| `pkg/cdn` | Content server pool, manifest request codes (incl. branch), depot keys, CDN auth tokens; HTTP fetch + AES decrypt + zstd/LZMA decompress + Adler-32 verify. |
| `pkg/manifest` | Parse/cache depot manifests (file→chunk model, filename decryption). |
| `pkg/workshop` | Resolve published-file ids (incl. collections) to depot+manifest via `PublishedFile.GetDetails` and the app's `workshopdepot`. |
| `pkg/app` | Resolve an AppID + branch to depots/manifests via PICS (OS/arch/language filters, shared depots, password-protected betas). |
| `pkg/sync` | The delta engine: diff old vs new manifest, reuse chunks by SHA-1 + Adler-32, prune deletions, atomic per-file staging. |
| `pkg/downloader` | Top-level orchestrator for workshop items and multi-depot app installs, persisting install state. |
| `pkg/state` | JSON-backed tokens, workshop + app installed-manifest maps, manifest cache. |
| `pkg/vdf`, `pkg/steamrpc` | KeyValues parsing and the synchronous CM job RPC helper. |

```go
sess, _ := client.Login(ctx, client.Options{Username: u, Password: p, Store: store, Guard: guardFn})
defer sess.Close()

// Workshop mods:
d, _ := downloader.New(ctx, sess, store, downloader.Config{AppID: 107410, OutDir: "mods", Prune: true})
results, _ := d.Download(ctx, []uint64{450814997})

// Full application (server files, etc.):
ad, _ := downloader.NewApp(ctx, sess, store, downloader.AppConfig{
    AppID: 233780, Branch: "profiling", OutDir: "server", Prune: true,
})
appResults, _ := ad.Download(ctx)
```

### Real-time progress (for a UI)

Set `Config.OnProgress` (workshop) or `AppConfig.OnProgress` (apps) to receive
live events. The CLI's progress bar is itself just a consumer of these callbacks.

```go
d, _ := downloader.New(ctx, sess, store, downloader.Config{
    AppID:  107410,
    OutDir: "mods",
    Prune:  true,
    OnProgress: func(p downloader.Progress) {
        switch p.Phase {
        case downloader.PhaseStarted:
            ui.AddRow(p.Item.PubFileID, p.Item.Title)
        case downloader.PhaseDownloading:
            ui.SetPercent(p.Item.PubFileID, p.Percent()) // p.CompletedBytes / p.TotalBytes
        case downloader.PhaseCompleted:
            ui.MarkDone(p.Item.PubFileID, p.Stats)       // bytes/chunks/files written, reused, deleted
        case downloader.PhaseFailed:
            ui.MarkFailed(p.Item.PubFileID, p.Err)
        }
    },
})

// Resolve first if you want the full item set up front (e.g. to size the UI):
items, _ := d.Resolve(ctx, ids)   // expands collections
results := d.DownloadItems(ctx, items)
```

Notes for UI integration:

- `OnProgress` is called **concurrently** from one goroutine per in-flight mod
  (`MaxConcurrentMods`). Your handler must be safe for concurrent use (a mutex
  or a channel into your UI thread).
- `PhaseDownloading` is throttled per item by `Config.ProgressInterval` (default
  100ms), so you can update the UI directly without being flooded.
- `Progress.Percent()` returns 0–100; `CompletedBytes` counts both downloaded
  **and** reused (delta) bytes, so the bar reflects real materialized progress,
  not just network traffic.
- `PhaseCompleted` carries a `sync.Stats` (downloaded/reused chunks, files
  written/unchanged/deleted) for a per-mod summary.
- It is pure Go (no cgo) and platform-agnostic — works the same on Linux.

## Vendored fork

`third_party/go-steam` is a copy of `0xAozora/go-steam` (BSD-3), wired in via a
`replace` directive so we own and patch it (QR login was added in
`auth_qr.go`; PICS private-beta messages in `protocol/protobuf/pics_private_beta.*`).
The CDN content path, server pool, zstd, tokens, workshop, and app logic are
reimplemented in `pkg/*` rather than using the fork's hardcoded-host `cdn.go`.

## Status / not yet done

- **Not yet tested against the live Steam network.** Unit tests cover the pure
  logic (Adler-32 seed-0, zstd framing, manifest parsing, VDF, app depot
  filtering, and the delta engine via a fake CDN). End-to-end download requires
  an account that owns the target app — see the verification steps below.
- Proxy/Lancache content servers are skipped (only direct servers are used).
- `file_url`-only workshop items (web-hosted, non-depot) are not downloaded.
- PICS product-info is read from a single response (no multi-part reassembly).
- App installs do not write Steam `appmanifest_*.acf` library metadata.

## Verifying end-to-end (manual)

1. `gosteam login --user NAME` → second run needs no password (token cached).
2. Download a small known mod (e.g. CBA_A3, `450814997`); confirm files appear
   under `mods/450814997/` and match SteamCMD's
   `workshop_download_item 107410 450814997` output.
3. Re-run: expect 0 chunks downloaded, all reused/unchanged.
4. Corrupt a file and re-run with `--verify-all`: the bad chunks are
   re-downloaded.
5. After a mod update, only changed chunks download and removed files are pruned.
6. `gosteam app-download --app 233780 --out ./server` and confirm the dedicated
   server layout matches a SteamCMD `app_update 233780` install; re-run should
   delta. Try `--branch profiling` if you use that beta.
