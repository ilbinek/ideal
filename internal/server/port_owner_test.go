package server

import (
	"net"
	"os"
	"strings"
	"testing"
)

func TestPortDecision_Retarget(t *testing.T) {
	state := classifyPort(true, 4242, identityMatch)
	if state != portOwned {
		t.Fatalf("state %v", state)
	}
	if v, polls := judgePort(state, 2); v != verdictRetarget || polls != 0 {
		t.Fatalf("verdict %v polls %d", v, polls)
	}
}

func TestPortDecision_Foreign(t *testing.T) {
	state := classifyPort(true, 4242, identityMismatch)
	if state != portForeign {
		t.Fatalf("state %v", state)
	}
	// Must read as dead straight away: this is the case that used to report a
	// server as running forever.
	if v, _ := judgePort(state, 0); v != verdictDead {
		t.Fatalf("verdict %v", v)
	}
}

func TestPortDecision_Free(t *testing.T) {
	state := classifyPort(false, 0, identityGone)
	if state != portFree {
		t.Fatalf("state %v", state)
	}
	if v, _ := judgePort(state, 0); v != verdictDead {
		t.Fatalf("verdict %v", v)
	}
}

func TestPortDecision_UnknownExpires(t *testing.T) {
	for _, state := range []portState{
		classifyPort(true, 0, identityGone),
		classifyPort(true, 4242, identityUnknown),
	} {
		if state != portOwnerUnknown {
			t.Fatalf("state %v", state)
		}
		polls := 0
		for i := 1; i < portUnknownLimit; i++ {
			var v portVerdict
			if v, polls = judgePort(state, polls); v != verdictHold {
				t.Fatalf("poll %d: verdict %v", i, v)
			}
		}
		if v, _ := judgePort(state, polls); v != verdictDead {
			t.Fatalf("final verdict %v", v)
		}
	}
}

func TestFindPortOwnerSelf(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	port := conn.LocalAddr().(*net.UDPAddr).Port

	if got := findPortOwner(port); got != os.Getpid() {
		t.Fatalf("owner %d, want %d", got, os.Getpid())
	}
	if desc := describePortHolder(port); !strings.Contains(desc, "held by") {
		t.Fatalf("description %q", desc)
	}
}
