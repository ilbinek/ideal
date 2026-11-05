package server

import (
	"os"
	"testing"
)

func armaWant(createMs int64) processWant {
	return processWant{CreateMs: createMs, Tokens: []string{"-port=2302"}}
}

func TestCheckIdentity_CreateTimeMatch(t *testing.T) {
	got := processFacts{CreateMs: 1_000_500, Name: "arma3server_x64.exe", Cmdline: "x -port=2302"}
	if r := checkIdentity(armaWant(1_000_000), got); r != identityMatch {
		t.Fatalf("got %v", r)
	}
	// Start time alone is conclusive, even with nothing else readable.
	if r := checkIdentity(armaWant(1_000_000), processFacts{CreateMs: 1_001_000}); r != identityMatch {
		t.Fatalf("create time only: got %v", r)
	}
}

func TestCheckIdentity_PidReused(t *testing.T) {
	cases := []struct {
		name string
		got  processFacts
	}{
		{"unrelated process", processFacts{CreateMs: 9_000_000, Name: "notepad.exe", Cmdline: "notepad.exe a.txt"}},
		{"other arma on another port", processFacts{CreateMs: 9_000_000, Name: "arma3server_x64.exe", Cmdline: "arma3server_x64.exe -port=2402"}},
		{"different start, nothing else readable", processFacts{CreateMs: 9_000_000}},
		{"different start, cmdline unreadable", processFacts{CreateMs: 9_000_000, Name: "arma3server_x64.exe"}},
	}
	for _, c := range cases {
		if r := checkIdentity(armaWant(1_000_000), c.got); r != identityMismatch {
			t.Errorf("%s: got %v", c.name, r)
		}
	}
}

func TestCheckIdentity_ClockShiftKeepsOwnProcess(t *testing.T) {
	// A stepped clock moves the Linux-derived start time, but the command line
	// still proves this is the server's own process.
	got := processFacts{CreateMs: 1_060_000, Name: "arma3server_x64", Cmdline: "./arma3server_x64 -port=2302 -name=Server"}
	if r := checkIdentity(armaWant(1_000_000), got); r != identityMatch {
		t.Fatalf("got %v", r)
	}
}

func TestCheckIdentity_LegacyCmdline(t *testing.T) {
	// Rows written before pid_create_ms existed have no start time to compare.
	got := processFacts{CreateMs: 5_000, Name: "arma3server_x64.exe", Cmdline: `arma3server_x64.exe "-port=2302" -config=server.cfg`}
	if r := checkIdentity(armaWant(0), got); r != identityMatch {
		t.Fatalf("own server: got %v", r)
	}
	got.Cmdline = "arma3server_x64.exe -port=23020"
	if r := checkIdentity(armaWant(0), got); r != identityMismatch {
		t.Fatalf("port is a prefix only: got %v", r)
	}
	got = processFacts{Name: "svchost.exe", Cmdline: "svchost.exe -k netsvcs"}
	if r := checkIdentity(armaWant(0), got); r != identityMismatch {
		t.Fatalf("foreign: got %v", r)
	}

	hc := processWant{Tokens: []string{"-client", "-name=Main-HC1"}}
	got = processFacts{Name: "arma3server_x64.exe", Cmdline: "x -client -connect=127.0.0.1 -port=2302 -name=Main-HC1"}
	if r := checkIdentity(hc, got); r != identityMatch {
		t.Fatalf("headless: got %v", r)
	}
	got.Cmdline = "x -client -connect=127.0.0.1 -port=2302 -name=Main-HC2"
	if r := checkIdentity(hc, got); r != identityMismatch {
		t.Fatalf("other headless slot: got %v", r)
	}
}

func TestCheckIdentity_CustomBinary(t *testing.T) {
	want := processWant{Binary: "custom/myserver.exe", Tokens: []string{"-port=2302"}}
	got := processFacts{Name: "myserver.exe", Cmdline: "myserver.exe -port=2302"}
	if r := checkIdentity(want, got); r != identityMatch {
		t.Fatalf("got %v", r)
	}
}

func TestCheckIdentity_Unreadable(t *testing.T) {
	if r := checkIdentity(armaWant(1_000_000), processFacts{}); r != identityUnknown {
		t.Fatalf("nothing readable: got %v", r)
	}
	if r := checkIdentity(armaWant(0), processFacts{Name: "arma3server_x64"}); r != identityUnknown {
		t.Fatalf("name only, no start time recorded: got %v", r)
	}
}

func TestReadProcessFactsSelf(t *testing.T) {
	facts := readProcessFacts(os.Getpid())
	if facts.CreateMs <= 0 || facts.Name == "" {
		t.Fatalf("facts: %+v", facts)
	}
	if ms := captureCreateMs(os.Getpid()); ms != facts.CreateMs {
		t.Fatalf("capture %d != %d", ms, facts.CreateMs)
	}
	if captureCreateMs(0) != 0 {
		t.Fatal("pid 0 should have no start time")
	}
}
