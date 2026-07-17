package server

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestNeedsForeignArchEmulator(t *testing.T) {
	want := runtime.GOOS == "linux" && runtime.GOARCH == "arm64"
	if NeedsForeignArchEmulator() != want {
		t.Fatalf("NeedsForeignArchEmulator() = %v, want %v", NeedsForeignArchEmulator(), want)
	}
}

func TestNormalizeEmulatorMode(t *testing.T) {
	cases := map[string]string{
		"":      EmulatorModeAuto,
		"AUTO":  EmulatorModeAuto,
		"box64": EmulatorModeBox64,
		"fex":   EmulatorModeFEX,
		"none":  EmulatorModeNone,
		"other": EmulatorModeAuto,
	}
	for in, want := range cases {
		if got := normalizeEmulatorMode(in); got != want {
			t.Errorf("normalizeEmulatorMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestApplyEmulatorWrapNoneOnNonArm(t *testing.T) {
	if NeedsForeignArchEmulator() {
		t.Skip("host needs emulator; wrap behavior differs")
	}
	la := &LaunchArgs{
		Binary:     filepath.Join("servers", "test", "arma3server_x64"),
		Args:       []string{"-port=2302"},
		WorkingDir: "servers/test",
		Command:    "./arma3server_x64 -port=2302",
	}
	origBin := la.Binary
	if err := ApplyEmulatorWrap(la); err != nil {
		t.Fatal(err)
	}
	if la.Binary != origBin {
		t.Fatalf("binary changed on native host: %q", la.Binary)
	}
	if len(la.Args) != 1 || la.Args[0] != "-port=2302" {
		t.Fatalf("args mutated: %#v", la.Args)
	}
}

func TestApplyEmulatorWrapManual(t *testing.T) {
	// Simulate wrap without requiring arm64 host.
	la := &LaunchArgs{
		Binary:     "/data/servers/s1/arma3server_x64",
		Args:       []string{"-port=2302", "-cfg=basic.cfg"},
		WorkingDir: "/data/servers/s1",
	}
	emu := "/usr/local/bin/box64"
	armaBinary := la.Binary
	la.Args = append([]string{armaBinary}, la.Args...)
	la.Binary = emu
	binName := filepath.Base(armaBinary)
	display := []string{filepath.Base(emu), "./" + filepath.ToSlash(binName)}
	display = append(display, formatCommandArgs(la.Args[1:])...)
	la.Command = joinForTest(display)

	if la.Binary != emu {
		t.Fatalf("binary = %q", la.Binary)
	}
	if la.Args[0] != armaBinary {
		t.Fatalf("args[0] = %q", la.Args[0])
	}
	if la.Command == "" || la.Command[:5] != "box64" {
		t.Fatalf("command = %q", la.Command)
	}
}

func joinForTest(parts []string) string {
	out := parts[0]
	for _, p := range parts[1:] {
		out += " " + p
	}
	return out
}
