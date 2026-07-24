//go:build linux

package server

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"arma3modmanager/internal/database"
	"arma3modmanager/internal/utils"
)

//go:embed dockerfiles/Dockerfile
var dockerRuntimeDockerfile []byte

const (
	dockerRuntimeImage = "ideal-arma-runtime:local"
	dockerLabelManaged = "ideal.managed=1"
	dockerLabelServer  = "ideal.server_id"
)

// DockerAvailable reports whether the docker CLI can talk to a local daemon.
func DockerAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "info")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

// UseDockerEffective is true when Settings has Docker enabled and the host can run it.
func UseDockerEffective() bool {
	cfg, err := database.GetConfig()
	if err != nil || cfg == nil || !cfg.UseDocker {
		return false
	}
	return DockerAvailable()
}

// dockerModeForStart returns whether this start should use Docker.
// If Docker is enabled in settings but unavailable, it returns an actionable error.
func dockerModeForStart() (bool, error) {
	cfg, err := database.GetConfig()
	if err != nil {
		return false, err
	}
	if cfg == nil || !cfg.UseDocker {
		return false, nil
	}
	if !DockerAvailable() {
		return false, fmt.Errorf("Docker isolation is enabled but Docker is not available (is the docker CLI installed and the daemon running?)")
	}
	return true, nil
}

func dockerContainerName(serverID int) string {
	return fmt.Sprintf("ideal-server-%d", serverID)
}

// EnsureDockerRuntimeImage builds ideal-arma-runtime:local if it is missing.
func EnsureDockerRuntimeImage() error {
	if !DockerAvailable() {
		return fmt.Errorf("Docker is not available (is the docker CLI installed and the daemon running?)")
	}
	if imageExists(dockerRuntimeImage) {
		return nil
	}
	return buildRuntimeImage()
}

func imageExists(name string) bool {
	cmd := exec.Command("docker", "image", "inspect", name)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

func buildRuntimeImage() error {
	buildDir, err := os.MkdirTemp("", "ideal-arma-runtime-*")
	if err != nil {
		return fmt.Errorf("create docker build dir: %w", err)
	}
	defer os.RemoveAll(buildDir)

	dfPath := filepath.Join(buildDir, "Dockerfile")
	if err := os.WriteFile(dfPath, dockerRuntimeDockerfile, 0644); err != nil {
		return fmt.Errorf("write Dockerfile: %w", err)
	}

	// Prefer repo Dockerfile when present (dev checkouts / releases that ship docker/).
	if hostDF := resolveHostDockerfile(); hostDF != "" {
		if data, err := os.ReadFile(hostDF); err == nil && len(data) > 0 {
			_ = os.WriteFile(dfPath, data, 0644)
		}
	}

	log.Printf("[DOCKER] building runtime image %s …", dockerRuntimeImage)
	cmd := exec.Command("docker", "build", "-t", dockerRuntimeImage, buildDir)
	var stderr bytes.Buffer
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("docker build failed for %s: %s", dockerRuntimeImage, msg)
	}
	return nil
}

func resolveHostDockerfile() string {
	candidates := []string{
		filepath.Join(utils.AppRoot(), "docker", "arma-runtime", "Dockerfile"),
		filepath.Join(utils.DataRoot(), "docker", "arma-runtime", "Dockerfile"),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func dockerRun(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("docker %s: %s", args[0], msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func startDockerServer(serverID int, binary string, args []string, workDir, sessionLogPath string, cpuAffinity string, memoryLimitMB int, extraMounts []string) (containerID string, proc *managedProc, err error) {
	if err := EnsureDockerRuntimeImage(); err != nil {
		return "", nil, err
	}

	name := dockerContainerName(serverID)
	_ = removeDockerContainer(name)

	dataRoot, err := filepath.Abs(utils.DataRoot())
	if err != nil {
		return "", nil, fmt.Errorf("resolve data root: %w", err)
	}
	workDirAbs, err := filepath.Abs(workDir)
	if err != nil {
		return "", nil, fmt.Errorf("resolve work dir: %w", err)
	}
	binAbs, err := filepath.Abs(binary)
	if err != nil {
		return "", nil, fmt.Errorf("resolve binary: %w", err)
	}
	logAbs, err := filepath.Abs(sessionLogPath)
	if err != nil {
		return "", nil, fmt.Errorf("resolve session log: %w", err)
	}

	// Run the binary as PID 1 so Docker captures stdout/stderr. On Linux, Arma's
	// "RPT" is printed to stdout — shell redirects would hide it from `docker logs`
	// and are easy to get wrong with quoting. We persist via docker logs → file.
	homeDir := filepath.Join(workDirAbs, "profiles")
	runArgs := []string{
		"run", "-d",
		"--name", name,
		"--label", dockerLabelManaged,
		"--label", fmt.Sprintf("%s=%d", dockerLabelServer, serverID),
		"--network", "host",
		"-e", "HOME=" + homeDir,
		"-v", dataRoot + ":" + dataRoot + ":rw",
		"-w", workDirAbs,
	}
	for _, m := range uniqueMounts(extraMounts, dataRoot) {
		runArgs = append(runArgs, "-v", m+":"+m+":ro")
	}
	if cpus := strings.TrimSpace(cpuAffinity); cpus != "" {
		runArgs = append(runArgs, "--cpuset-cpus", cpus)
	}
	if memoryLimitMB > 0 {
		runArgs = append(runArgs,
			"--memory", fmt.Sprintf("%dm", memoryLimitMB),
			"--memory-swap", fmt.Sprintf("%dm", memoryLimitMB*2),
		)
	}
	runArgs = append(runArgs, dockerRuntimeImage, binAbs)
	runArgs = append(runArgs, args...)

	containerID, err = dockerRun(runArgs...)
	if err != nil {
		return "", nil, fmt.Errorf("failed to start Docker container for server %d: %w", serverID, err)
	}

	pid, err := dockerContainerPID(name)
	if err != nil {
		_ = stopDockerContainer(serverID, "")
		return "", nil, fmt.Errorf("container started but PID unavailable: %w", err)
	}

	go followDockerLogsToFile(name, logAbs)
	return containerID, watchDockerContainer(name, pid), nil
}

func uniqueMounts(paths []string, dataRoot string) []string {
	seen := map[string]struct{}{filepath.Clean(dataRoot): {}}
	var out []string
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		abs = filepath.Clean(abs)
		if _, ok := seen[abs]; ok {
			continue
		}
		// Skip paths already under DataRoot.
		if strings.HasPrefix(abs, filepath.Clean(dataRoot)+string(os.PathSeparator)) {
			continue
		}
		if st, err := os.Stat(abs); err != nil || !st.IsDir() {
			continue
		}
		seen[abs] = struct{}{}
		out = append(out, abs)
	}
	return out
}

func watchDockerContainer(containerName string, pid int) *managedProc {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			running, err := dockerInspectRunning(containerName)
			if err != nil || !running {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
	return &managedProc{pid: pid, done: done}
}

// followDockerLogsToFile appends container stdout/stderr into the session log on the host.
// Linux Arma writes its RPT to stdout, so this is the durable historic log.
func followDockerLogsToFile(containerName, logPath string) {
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("[DOCKER] open session log %s: %v", logPath, err)
		return
	}
	defer f.Close()

	// No --since: include everything emitted since container start (covers the
	// small race between `docker run` and this follow starting).
	cmd := exec.Command("docker", "logs", "-f", containerName)
	cmd.Stdout = f
	cmd.Stderr = f
	_ = cmd.Run()
	_ = f.Sync()
}

// flushDockerLogsToFile rewrites the session log from a full `docker logs` dump.
// Call after docker stop and before docker rm so historic logs survive removal.
func flushDockerLogsToFile(containerName, logPath string) {
	if strings.TrimSpace(logPath) == "" || !dockerContainerExists(containerName) {
		return
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Printf("[DOCKER] flush session log %s: %v", logPath, err)
		return
	}
	defer f.Close()

	cmd := exec.Command("docker", "logs", containerName)
	cmd.Stdout = f
	cmd.Stderr = f
	if err := cmd.Run(); err != nil {
		log.Printf("[DOCKER] docker logs %s: %v", containerName, err)
		return
	}
	_ = f.Sync()
}

func dockerInspectRunning(nameOrID string) (bool, error) {
	out, err := dockerRun("inspect", "-f", "{{.State.Running}}", nameOrID)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "true", nil
}

func dockerContainerPID(nameOrID string) (int, error) {
	out, err := dockerRun("inspect", "-f", "{{.State.Pid}}", nameOrID)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid container pid %q", out)
	}
	return pid, nil
}

func removeDockerContainer(name string) error {
	cmd := exec.Command("docker", "rm", "-f", name)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	_ = cmd.Run()
	return nil
}

func stopDockerContainer(serverID int, sessionLogPath string) error {
	name := dockerContainerName(serverID)
	cmd := exec.Command("docker", "stop", "-t", "15", name)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Already gone is fine.
		if !dockerContainerExists(name) {
			return nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("docker stop %s: %s", name, msg)
	}
	// Let the followDockerLogsToFile goroutine exit before we rewrite the file.
	time.Sleep(300 * time.Millisecond)
	// Persist stdout/stderr before rm — Linux Arma RPT lives in docker logs.
	flushDockerLogsToFile(name, sessionLogPath)
	_ = removeDockerContainer(name)
	return nil
}

func dockerContainerExists(name string) bool {
	cmd := exec.Command("docker", "inspect", name)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

func dockerContainerRunning(serverID int) bool {
	running, err := dockerInspectRunning(dockerContainerName(serverID))
	return err == nil && running
}

func findDockerContainer(serverID int) (containerID string, pid int, running bool, err error) {
	name := dockerContainerName(serverID)
	if !dockerContainerExists(name) {
		// Fallback: label filter.
		out, listErr := dockerRun("ps", "-aq", "--filter", "label="+dockerLabelManaged,
			"--filter", fmt.Sprintf("label=%s=%d", dockerLabelServer, serverID))
		if listErr != nil || strings.TrimSpace(out) == "" {
			return "", 0, false, nil
		}
		name = strings.Fields(out)[0]
	}
	id, err := dockerRun("inspect", "-f", "{{.Id}}", name)
	if err != nil {
		return "", 0, false, nil
	}
	running, _ = dockerInspectRunning(name)
	if running {
		pid, _ = dockerContainerPID(name)
	}
	return id, pid, running, nil
}

func adoptDockerContainer(serverID int) (containerID string, proc *managedProc, pid int, err error) {
	id, pid, running, err := findDockerContainer(serverID)
	if err != nil {
		return "", nil, 0, err
	}
	if !running || pid <= 0 {
		return "", nil, 0, fmt.Errorf("no running docker container for server %d", serverID)
	}
	return id, watchDockerContainer(dockerContainerName(serverID), pid), pid, nil
}

// execDockerHeadless starts an HC inside the server's container and returns the host PID when possible.
// Stdout/stderr are redirected into sessionLogPath on the bind mount (no host-side self-tail).
func execDockerHeadless(serverID int, binary string, args []string, workDir, sessionLogPath string) (pid int, proc *managedProc, err error) {
	name := dockerContainerName(serverID)
	if !dockerContainerRunning(serverID) {
		return 0, nil, fmt.Errorf("server container %s is not running", name)
	}

	binAbs, err := filepath.Abs(binary)
	if err != nil {
		return 0, nil, err
	}
	workAbs, err := filepath.Abs(workDir)
	if err != nil {
		return 0, nil, err
	}
	logAbs, err := filepath.Abs(sessionLogPath)
	if err != nil {
		return 0, nil, err
	}
	pidFile := logAbs + ".pid"

	// Append so a host-created empty session file keeps its identity; avoid
	// truncating if Ideal already opened the path.
	shellCmd := fmt.Sprintf(
		"cd %s && %s %s >> %s 2>&1 & echo $! > %s; cat %s",
		shellQuote(workAbs),
		shellQuote(binAbs),
		shellJoinArgs(args),
		shellQuote(logAbs),
		shellQuote(pidFile),
		shellQuote(pidFile),
	)

	out, err := dockerRun("exec", name, "sh", "-c", shellCmd)
	if err != nil {
		return 0, nil, fmt.Errorf("docker exec headless: %w", err)
	}
	innerPID, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || innerPID <= 0 {
		return 0, nil, fmt.Errorf("failed to parse headless pid from docker exec: %q", out)
	}

	hostPID := resolveHostPIDFromContainer(name, innerPID)
	if hostPID <= 0 {
		hostPID = innerPID
		proc = watchDockerExecPID(name, innerPID)
	} else {
		proc = adoptProcess(hostPID)
	}
	return hostPID, proc, nil
}

func stopDockerHeadless(serverID, pid int) error {
	if pid <= 0 {
		return nil
	}
	name := dockerContainerName(serverID)
	// Prefer signaling inside the container (works for both host and namespace PIDs when pid host-visible).
	if processRunning(pid) {
		return stopProcess(pid)
	}
	_, _ = dockerRun("exec", name, "kill", "-TERM", strconv.Itoa(pid))
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !dockerExecPIDAlive(name, pid) && !processRunning(pid) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	_, _ = dockerRun("exec", name, "kill", "-KILL", strconv.Itoa(pid))
	_ = stopProcess(pid)
	return nil
}

func watchDockerExecPID(containerName string, innerPID int) *managedProc {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for dockerExecPIDAlive(containerName, innerPID) {
			time.Sleep(500 * time.Millisecond)
		}
	}()
	return &managedProc{pid: innerPID, done: done}
}

func dockerExecPIDAlive(containerName string, pid int) bool {
	cmd := exec.Command("docker", "exec", containerName, "kill", "-0", strconv.Itoa(pid))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

func dockerExecStillAlive(serverID, pid int) bool {
	if pid <= 0 {
		return false
	}
	if processRunning(pid) {
		return true
	}
	return dockerExecPIDAlive(dockerContainerName(serverID), pid)
}

func resolveHostPIDFromContainer(containerName string, innerPID int) int {
	// docker top shows host PIDs; match by reading /proc/<host>/status NSpid.
	out, err := dockerRun("top", containerName, "-eo", "pid")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "PID") {
			continue
		}
		hostPID, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		if nspidsContain(hostPID, innerPID) {
			return hostPID
		}
	}
	return 0
}

func nspidsContain(hostPID, innerPID int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", hostPID))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "NSpid:") {
			continue
		}
		fields := strings.Fields(line)
		for _, f := range fields[1:] {
			if n, err := strconv.Atoi(f); err == nil && n == innerPID {
				return true
			}
		}
	}
	return false
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func shellJoinArgs(args []string) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

// CollectDockerExtraMounts returns absolute dirs outside DataRoot that must be bind-mounted
// so absolute symlinks from custom-mod mirrors resolve inside the container.
func CollectDockerExtraMounts(serverID int) []string {
	mods, err := database.GetServerMods(serverID)
	if err != nil {
		return nil
	}
	dataRoot := filepath.Clean(utils.DataRoot())
	var mounts []string
	for _, sm := range mods {
		if !sm.Enabled || sm.CustomPath == "" {
			continue
		}
		abs, err := filepath.Abs(sm.CustomPath)
		if err != nil {
			continue
		}
		abs = filepath.Clean(abs)
		if strings.HasPrefix(abs, dataRoot+string(os.PathSeparator)) || abs == dataRoot {
			continue
		}
		mounts = append(mounts, abs)
	}
	return mounts
}
