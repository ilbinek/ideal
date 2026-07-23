//go:build !linux

package server

import (
	"fmt"
	"io"
)

const dockerRuntimeImage = "ideal-arma-runtime:local"

// DockerAvailable reports whether Docker isolation can be used on this host.
func DockerAvailable() bool { return false }

// EnsureDockerRuntimeImage is a no-op off Linux.
func EnsureDockerRuntimeImage() error {
	return fmt.Errorf("Docker isolation is only supported on Linux")
}

// UseDockerEffective is always false off Linux.
func UseDockerEffective() bool { return false }

func dockerModeForStart() (bool, error) {
	// useDocker in the DB is ignored off Linux so a shared database cannot block starts.
	return false, nil
}

func dockerExecStillAlive(serverID, pid int) bool { return false }

func dockerContainerName(serverID int) string {
	return fmt.Sprintf("ideal-server-%d", serverID)
}

func startDockerServer(serverID int, binary string, args []string, workDir string, cpuAffinity string, memoryLimitMB int, extraMounts []string, capture io.Writer) (containerID string, proc *managedProc, err error) {
	return "", nil, fmt.Errorf("Docker isolation is only supported on Linux")
}

func execDockerHeadless(serverID int, binary string, args []string, workDir, sessionLogPath string, capture io.Writer) (pid int, proc *managedProc, err error) {
	return 0, nil, fmt.Errorf("Docker isolation is only supported on Linux")
}

func stopDockerContainer(serverID int) error {
	return fmt.Errorf("Docker isolation is only supported on Linux")
}

func stopDockerHeadless(serverID, pid int) error {
	return fmt.Errorf("Docker isolation is only supported on Linux")
}

func findDockerContainer(serverID int) (containerID string, pid int, running bool, err error) {
	return "", 0, false, nil
}

func adoptDockerContainer(serverID int) (containerID string, proc *managedProc, pid int, err error) {
	return "", nil, 0, fmt.Errorf("Docker isolation is only supported on Linux")
}

func dockerContainerRunning(serverID int) bool { return false }

func CollectDockerExtraMounts(serverID int) []string { return nil }
