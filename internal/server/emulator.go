package server

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"arma3modmanager/internal/database"
)

// EmulatorMode values stored in settings.
const (
	EmulatorModeAuto  = "auto"
	EmulatorModeBox64 = "box64"
	EmulatorModeFEX   = "fex"
	EmulatorModeNone  = "none"
)

// EmulatorStatus is the ARM/x86_64 compatibility probe returned to the UI.
type EmulatorStatus struct {
	GOOS             string `json:"goos"`
	GOARCH           string `json:"goarch"`
	NeedsEmulator    bool   `json:"needsEmulator"`
	Mode             string `json:"mode"`
	Resolved         string `json:"resolved"` // box64 | fex | none | ""
	Path             string `json:"path"`
	Version          string `json:"version"`
	Ready            bool   `json:"ready"`
	Message          string `json:"message"`
	InstallHint      string `json:"installHint"`
	Experimental     bool   `json:"experimental"`
	CanInstallBox64  bool   `json:"canInstallBox64"`
	ManagedBox64Path string `json:"managedBox64Path"`
	Box64Managed     bool   `json:"box64Managed"`
}

// NeedsForeignArchEmulator reports whether Arma dedicated binaries need an
// x86_64 userspace emulator on this host (Linux arm64).
func NeedsForeignArchEmulator() bool {
	return runtime.GOOS == "linux" && runtime.GOARCH == "arm64"
}

func normalizeEmulatorMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case EmulatorModeBox64, EmulatorModeFEX, EmulatorModeNone:
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return EmulatorModeAuto
	}
}

func emulatorSettings() (mode, box64Path, fexPath string) {
	mode = EmulatorModeAuto
	if cfg, err := database.GetConfig(); err == nil && cfg != nil {
		mode = normalizeEmulatorMode(cfg.EmulatorMode)
		box64Path = strings.TrimSpace(cfg.Box64Path)
		fexPath = strings.TrimSpace(cfg.FexPath)
	}
	return mode, box64Path, fexPath
}

func lookPathOr(explicit, name string) (string, error) {
	if explicit != "" {
		if st, err := os.Stat(explicit); err == nil && !st.IsDir() {
			return explicit, nil
		}
		return "", fmt.Errorf("%s not found at %q", name, explicit)
	}
	return exec.LookPath(name)
}

func resolveBox64(explicit string) (path, version string, err error) {
	if explicit != "" {
		path, err = lookPathOr(explicit, "box64")
		if err != nil {
			return "", "", err
		}
		version = probeVersion(path, "--version")
		return path, version, nil
	}
	if p, v, ok := managedBox64Available(); ok {
		return p, v, nil
	}
	path, err = exec.LookPath("box64")
	if err != nil {
		return "", "", err
	}
	version = probeVersion(path, "--version")
	return path, version, nil
}

func resolveFEX(explicit string) (path, version string, err error) {
	if explicit != "" {
		path, err = lookPathOr(explicit, "FEXInterpreter")
		if err != nil {
			return "", "", err
		}
		version = probeVersion(path, "--version")
		return path, version, nil
	}
	for _, name := range []string{"FEXInterpreter", "fex"} {
		if p, e := exec.LookPath(name); e == nil {
			return p, probeVersion(p, "--version"), nil
		}
	}
	return "", "", fmt.Errorf("FEXInterpreter not found on PATH")
}

func probeVersion(bin string, args ...string) string {
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if len(line) > 120 {
		line = line[:120]
	}
	return line
}

// ResolveEmulator picks the userspace emulator for launching x86_64 Arma binaries.
// On non-arm64 Linux hosts it returns resolved "none" with no error.
func ResolveEmulator() (resolved, path, version string, err error) {
	if !NeedsForeignArchEmulator() {
		return EmulatorModeNone, "", "", nil
	}
	mode, box64Path, fexPath := emulatorSettings()
	if mode == EmulatorModeNone {
		return EmulatorModeNone, "", "", nil
	}

	tryBox := mode == EmulatorModeAuto || mode == EmulatorModeBox64
	tryFEX := mode == EmulatorModeAuto || mode == EmulatorModeFEX

	if tryBox {
		if p, v, e := resolveBox64(box64Path); e == nil {
			return EmulatorModeBox64, p, v, nil
		} else if mode == EmulatorModeBox64 {
			return "", "", "", fmt.Errorf("box64 required for ARM64 Arma servers: %w", e)
		}
	}
	if tryFEX {
		if p, v, e := resolveFEX(fexPath); e == nil {
			return EmulatorModeFEX, p, v, nil
		} else if mode == EmulatorModeFEX {
			return "", "", "", fmt.Errorf("FEX required for ARM64 Arma servers: %w", e)
		}
	}
	return "", "", "", fmt.Errorf("no x86_64 emulator found (install box64, or set emulator mode / path in Settings)")
}

// ApplyEmulatorWrap rewrites launch Binary/Args/Command for arm64 hosts.
func ApplyEmulatorWrap(la *LaunchArgs) error {
	if la == nil {
		return nil
	}
	resolved, emuPath, _, err := ResolveEmulator()
	if err != nil {
		return err
	}
	if resolved == EmulatorModeNone || emuPath == "" {
		return nil
	}

	armaBinary := la.Binary
	la.Args = append([]string{armaBinary}, la.Args...)
	la.Binary = emuPath

	binName := filepath.Base(armaBinary)
	display := []string{filepath.Base(emuPath), "./" + filepath.ToSlash(binName)}
	display = append(display, formatCommandArgs(la.Args[1:])...)
	la.Command = strings.Join(display, " ")
	return nil
}

// EmulatorProcessEnv returns extra env vars for the server process when wrapping.
func EmulatorProcessEnv() []string {
	resolved, _, _, err := ResolveEmulator()
	if err != nil || resolved != EmulatorModeBox64 {
		return nil
	}
	// Strong memory helps some titles on ARM; keep logging quiet by default.
	return []string{
		"BOX64_DYNAREC_STRONGMEM=1",
		"BOX64_LOG=0",
	}
}

// ProbeEmulatorStatus builds Settings / API compatibility info.
func ProbeEmulatorStatus() EmulatorStatus {
	mode, box64Path, fexPath := emulatorSettings()
	st := EmulatorStatus{
		GOOS:             runtime.GOOS,
		GOARCH:           runtime.GOARCH,
		NeedsEmulator:    NeedsForeignArchEmulator(),
		Mode:             mode,
		Experimental:     true,
		CanInstallBox64:  NeedsForeignArchEmulator(),
		ManagedBox64Path: ManagedBox64Path(),
	}
	if _, _, ok := managedBox64Available(); ok {
		st.Box64Managed = true
	}

	if !st.NeedsEmulator {
		st.Resolved = EmulatorModeNone
		st.Ready = true
		st.CanInstallBox64 = false
		st.Message = "Host can run Arma 3 dedicated binaries natively (no emulator)."
		return st
	}

	if mode == EmulatorModeNone {
		st.Resolved = EmulatorModeNone
		st.Ready = false
		st.Message = "ARM64 host: emulator disabled in Settings. Arma server binaries are x86_64 and will not start."
		st.InstallHint = box64InstallHint()
		return st
	}

	resolved, path, version, err := ResolveEmulator()
	if err != nil {
		st.Ready = false
		st.Message = err.Error()
		st.InstallHint = box64InstallHint()
		if mode == EmulatorModeFEX || (mode == EmulatorModeAuto && box64Path == "" && fexPath != "") {
			st.InstallHint = fexInstallHint()
		}
		return st
	}

	st.Resolved = resolved
	st.Path = path
	st.Version = version
	st.Ready = true
	st.Box64Managed = resolved == EmulatorModeBox64 && path == ManagedBox64Path()
	st.Message = fmt.Sprintf("ARM64 host: will launch Arma via %s (experimental).", resolved)
	st.InstallHint = box64InstallHint()
	return st
}

func box64InstallHint() string {
	return "Click “Download Box64” in Settings to install a private copy under Ideal’s data directory (no sudo). Or install system-wide (apt/box64-debs). Prefer gosteam for downloads. See scripts/arm64-spike/."
}

func fexInstallHint() string {
	return "Install FEX-Emu and an x86_64 RootFS (https://fex-emu.org/). Use only if Box64 cannot start arma3server_x64."
}
