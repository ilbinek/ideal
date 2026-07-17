# ARM64 Arma 3 emulation spike results

Generated: 2026-07-17T16:53:00Z  
Status: **BLOCKED** (no aarch64 Linux host in the Ideal development environment)

## Host (where this investigation ran)

| Field | Value |
|-------|-------|
| OS | Windows 10/11 |
| Arch | AMD64 (x86_64) |
| Docker | not available |
| WSL | not installed |

Phase A requires a Linux **aarch64** machine to install Box64 and exec `arma3server_x64`. That could not be done on this workstation.

## Prior art (research)

| Source | Finding |
|--------|---------|
| Bohemia Dedicated Server docs | Linux server binary is x86_64 (`arma3server_x64`) only — no ARM build |
| [box86 #120](https://github.com/ptitSeb/box86/issues/120) | Old **32-bit** `arma3server` segfaulted under Box86 on Pi (2020) — does not prove/disprove modern x64+Box64 |
| CubeCoders AMP AArch64 list | Many Steam servers run via Box64 “CPx”; **Arma 3 is not listed as working** (related “failing to launch” threads) |
| steamcmd-arm64 Docker images | Box64/Box32 stack is the common pattern for SteamCMD + x86_64 game servers on ARM |

## Emulators

Not exercised on aarch64 in this run.

| Tool | Role |
|------|------|
| **Box64** | Primary target: `box64 ./arma3server_x64 …` |
| **FEX-Emu** | Fallback if Box64 crashes (needs x86_64 RootFS) |
| **QEMU user/binfmt** | Last resort; usually slower |

## FEX fallback procedure (when Box64 fails)

1. Install FEX and fetch an x86_64 RootFS (`FEXRootFSFetcher`).
2. Retry: `FEXInterpreter ./arma3server_x64 -name=spike -config=server.cfg -port=2302`
3. If SteamAPI fails, symlink `linux64/steamclient.so` under `~/.steam/sdk64/` as on x86 hosts.
4. Keep BattlEye disabled for the spike (`BattlEye = 0;` in `server.cfg`).
5. Prefer Ideal/gosteam for app `233780` so 32-bit SteamCMD is not required.

Common failure modes to capture in logs: BattlEye init, missing `steamclient.so` / `libsteam_api.so`, segfault, illegal instruction, “cannot open shared object”.

## How to finish Phase A on real hardware

On a Linux aarch64 host (prefer server-class ARM over Raspberry Pi):

```bash
# Install Box64, then download Arma dedicated (233780) via Ideal/gosteam or SteamCMD+Box32
chmod +x scripts/arm64-spike/run-spike.sh
./scripts/arm64-spike/run-spike.sh /path/to/arma3/install
# If Box64 fails:
SPIKE_FEX=1 ./scripts/arm64-spike/run-spike.sh /path/to/arma3/install
```

Success criteria:

- [ ] Process stays up >5 minutes
- [ ] Game port binds; RPT shows mission/world load
- [ ] One x86 client joins and moves
- [ ] Optional: headless under same emulator; light AI without sub-10 server FPS

## Implication for Ideal

Ideal already builds `ideal-linux-arm64` and can download Linux x86_64 server files. Without a live PASSED spike on real aarch64 hardware, ARM game hosting remains **experimental**.

Ideal now implements automatic Box64/FEX launch wrapping:

- Detects Linux arm64 and wraps `arma3server_x64` / headless launches
- Settings: emulator mode + optional Box64/FEX paths; `GET /api/system/compat` readiness
- README documents experimental status and the spike harness

Re-run this script on aarch64 and replace this file’s Status with PASSED / PASSED_FEX / FAILED when you have results.
