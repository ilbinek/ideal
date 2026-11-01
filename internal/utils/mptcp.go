package utils

import (
	"os"
	"strings"
)

// HostMPTCPEnabled reports whether the host kernel has Multipath TCP enabled
// (net.mptcp.enabled = 1). It reads /proc/sys/net/mptcp/enabled and returns
// false on any error, so the check is silent on non-Linux hosts and on kernels
// without MPTCP support.
func HostMPTCPEnabled() bool {
	b, err := os.ReadFile("/proc/sys/net/mptcp/enabled")
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(b)) == "1"
}
