package server

import (
	"os"
	"testing"
)

// TestMain keeps every port check on loopback. Each test build is a new program to
// the OS firewall, so binding all interfaces would prompt on every run.
func TestMain(m *testing.M) {
	portProbeHost = "127.0.0.1"
	os.Exit(m.Run())
}
