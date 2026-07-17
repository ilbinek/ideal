#!/usr/bin/env bash
# Phase A validation: run Arma 3 dedicated server (x86_64) under Box64 on aarch64 Linux.
# Usage:
#   ./run-spike.sh                  # env check + box64 only
#   ./run-spike.sh /path/to/arma3   # also try launching arma3server_x64
#   SPIKE_FEX=1 ./run-spike.sh ...  # retry with FEXInterpreter if Box64 fails
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
RESULTS="${ROOT}/RESULTS.md"
ARMA_DIR="${1:-}"
STAY_UP_SECS="${SPIKE_STAY_UP:-30}"
PORT="${SPIKE_PORT:-2302}"

arch="$(uname -m)"
os="$(uname -s)"
box64_bin=""
box64_ver=""
fex_bin=""
fex_ver=""
launch_ok=0
launch_backend=""
notes=()

log() { printf '%s\n' "$*"; }
fail_note() { notes+=("$*"); }

require_aarch64() {
  if [[ "$arch" != "aarch64" && "$arch" != "arm64" ]]; then
    fail_note "Host is ${arch}, not aarch64 — cannot validate Box64/FEX for Arma."
    return 1
  fi
  return 0
}

find_box64() {
  if command -v box64 >/dev/null 2>&1; then
    box64_bin="$(command -v box64)"
    box64_ver="$("$box64_bin" --version 2>&1 | head -n1 || true)"
    return 0
  fi
  return 1
}

find_fex() {
  for c in FEXInterpreter fex-emu-install fex; do
    if command -v "$c" >/dev/null 2>&1; then
      fex_bin="$(command -v "$c")"
      fex_ver="$("$fex_bin" --version 2>&1 | head -n1 || true)"
      return 0
    fi
  done
  return 1
}

install_hint_box64() {
  cat <<'EOF'
Install Box64 (Debian/Ubuntu example):
  sudo apt-get update && sudo apt-get install -y box64
Or build from https://github.com/ptitSeb/box64 (enable ARM_DYNAREC).
Prefer gosteam/Ideal for downloads so you do not need 32-bit SteamCMD under Box32.
EOF
}

install_hint_fex() {
  cat <<'EOF'
Install FEX-Emu + RootFS:
  See https://fex-emu.org/ — typically FEXRootFSFetcher then FEXInterpreter.
Heavier than Box64; use only if Box64 cannot start arma3server_x64.
EOF
}

find_server_bin() {
  local dir="$1"
  for name in arma3server_x64 arma3server; do
    if [[ -x "${dir}/${name}" ]]; then
      printf '%s\n' "${dir}/${name}"
      return 0
    fi
  done
  # SteamCMD layout
  local found
  found="$(find "$dir" -maxdepth 4 -type f \( -name 'arma3server_x64' -o -name 'arma3server' \) 2>/dev/null | head -n1 || true)"
  if [[ -n "$found" ]]; then
    printf '%s\n' "$found"
    return 0
  fi
  return 1
}

ensure_min_cfg() {
  local dir="$1"
  if [[ ! -f "${dir}/server.cfg" ]]; then
    cat >"${dir}/server.cfg" <<'EOF'
hostname = "Ideal ARM spike";
password = "";
passwordAdmin = "spikeadmin";
maxPlayers = 4;
BattlEye = 0;
verifySignatures = 0;
EOF
  fi
  if [[ ! -f "${dir}/basic.cfg" ]]; then
    cat >"${dir}/basic.cfg" <<'EOF'
MaxMsgSend = 128;
MinErrorToSend = 0.001;
EOF
  fi
  mkdir -p "${dir}/profiles"
}

try_launch() {
  local emu="$1"
  local bin="$2"
  local work
  work="$(dirname "$bin")"
  ensure_min_cfg "$work"
  local logf
  logf="$(mktemp)"
  local cmd=()
  if [[ "$emu" == "box64" ]]; then
    cmd=("$box64_bin" "$bin" "-name=spike" "-config=server.cfg" "-cfg=basic.cfg" "-profiles=profiles" "-port=${PORT}")
    export BOX64_LOG=1
    export BOX64_DYNAREC_STRONGMEM="${BOX64_DYNAREC_STRONGMEM:-1}"
  else
    cmd=("$fex_bin" "$bin" "-name=spike" "-config=server.cfg" "-cfg=basic.cfg" "-profiles=profiles" "-port=${PORT}")
  fi

  log "Launching: ${cmd[*]}"
  (
    cd "$work"
    "${cmd[@]}"
  ) >"$logf" 2>&1 &
  local pid=$!
  sleep "$STAY_UP_SECS"
  if kill -0 "$pid" 2>/dev/null; then
    launch_ok=1
    launch_backend="$emu"
    log "Process still alive after ${STAY_UP_SECS}s (pid=$pid)."
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    tail -n 40 "$logf" || true
    rm -f "$logf"
    return 0
  fi
  fail_note "${emu} launch exited early. Last log lines:"
  tail -n 80 "$logf" || true
  # Capture common failure modes
  if grep -qiE 'battleye|steamapi|steamclient|segfault|illegal instruction|cannot open shared object' "$logf"; then
    fail_note "Likely BattlEye/SteamAPI/shared-lib failure under ${emu} (see log excerpt above)."
  fi
  rm -f "$logf"
  return 1
}

write_results() {
  local status="$1"
  cat >"$RESULTS" <<EOF
# ARM64 Arma 3 emulation spike results

Generated: $(date -u +%Y-%m-%dT%H:%M:%SZ)
Status: **${status}**

## Host

| Field | Value |
|-------|-------|
| OS | ${os} |
| Arch | ${arch} |
| Kernel | $(uname -r 2>/dev/null || echo n/a) |
| CPU | $(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2- | xargs || echo n/a) |

## Emulators

| Tool | Path | Version |
|------|------|---------|
| Box64 | ${box64_bin:-not found} | ${box64_ver:-} |
| FEX | ${fex_bin:-not found} | ${fex_ver:-} |

## Launch

| Field | Value |
|-------|-------|
| Arma dir | ${ARMA_DIR:-not provided} |
| Backend | ${launch_backend:-none} |
| Stayed up ${STAY_UP_SECS}s | $([[ $launch_ok -eq 1 ]] && echo yes || echo no) |

## Notes

$(if ((${#notes[@]})); then printf -- '- %s\n' "${notes[@]}"; else echo '- (none)'; fi)

## FEX fallback (when Box64 fails)

1. Install FEX and fetch an x86_64 RootFS (\`FEXRootFSFetcher\`).
2. Retry: \`FEXInterpreter ./arma3server_x64 -name=spike -config=server.cfg -port=${PORT}\`
3. If SteamAPI fails, ensure \`linux64/steamclient.so\` is reachable (symlink under \`~/.steam/sdk64/\` as on x86 hosts).
4. Keep BattlEye disabled for the spike (\`BattlEye = 0;\` in server.cfg).
5. Prefer Ideal/gosteam for app \`233780\` downloads so SteamCMD (32-bit) is not required.

## Success criteria (manual follow-up)

- [ ] Process up >5 minutes
- [ ] Game port binds; RPT shows mission/world load
- [ ] One x86 client joins and moves
- [ ] Optional: headless under same emulator; light AI without sub-10 server FPS

EOF
  log "Wrote ${RESULTS}"
}

main() {
  log "== Ideal ARM64 spike =="
  if ! require_aarch64; then
    find_box64 || true
    find_fex || true
    write_results "BLOCKED"
    exit 2
  fi

  if ! find_box64; then
    fail_note "box64 not on PATH."
    install_hint_box64 | while read -r line; do fail_note "$line"; done
  fi
  find_fex || true

  if [[ -z "$ARMA_DIR" ]]; then
    fail_note "No Arma install dir argument — skipped server launch (env/box64 probe only)."
    if [[ -n "$box64_bin" ]]; then
      write_results "PARTIAL"
      exit 0
    fi
    write_results "FAILED"
    exit 1
  fi

  if [[ ! -d "$ARMA_DIR" ]]; then
    fail_note "Arma dir does not exist: $ARMA_DIR"
    write_results "FAILED"
    exit 1
  fi

  local bin
  if ! bin="$(find_server_bin "$ARMA_DIR")"; then
    fail_note "arma3server_x64 / arma3server not found under $ARMA_DIR"
    write_results "FAILED"
    exit 1
  fi
  log "Found server binary: $bin"

  if [[ -n "$box64_bin" ]]; then
    if try_launch box64 "$bin"; then
      write_results "PASSED"
      exit 0
    fi
  fi

  if [[ "${SPIKE_FEX:-0}" == "1" || "${SPIKE_FEX:-}" == "true" ]]; then
    if [[ -z "$fex_bin" ]]; then
      fail_note "SPIKE_FEX set but FEXInterpreter not found."
      install_hint_fex | while read -r line; do fail_note "$line"; done
    elif try_launch fex "$bin"; then
      write_results "PASSED_FEX"
      exit 0
    fi
  else
    fail_note "Box64 launch failed. Re-run with SPIKE_FEX=1 to try FEX."
  fi

  write_results "FAILED"
  exit 1
}

main "$@"
