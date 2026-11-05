//go:build !linux

package server

import (
	"fmt"
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

func startDockerServer(serverID int, binary string, args []string, workDir, sessionLogPath string, cpuAffinity string, memoryLimitMB int, extraMounts []string) (containerID string, proc *managedProc, err error) {
	return "", nil, fmt.Errorf("Docker isolation is only supported on Linux")
}

func execDockerHeadless(serverID int, binary string, args []string, workDir, sessionLogPath string) (pid int, proc *managedProc, err error) {
	return 0, nil, fmt.Errorf("Docker isolation is only supported on Linux")
}

const dockerStopSeconds = 15

func stopDockerContainer(serverID int, sessionLogPath string) error {
	return fmt.Errorf("Docker isolation is only supported on Linux")
}

func stopDockerContainerAfter(serverID int, sessionLogPath string, waitSeconds int) error {
	return fmt.Errorf("Docker isolation is only supported on Linux")
}

func signalDockerContainer(serverID int) bool { return false }

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
