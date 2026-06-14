package depot

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const DedicatedServerAppID = "233780"

// AppUpdateOptions configures a SteamCMD app_update run.
type AppUpdateOptions struct {
	AppID        string
	SteamBranch  string // empty = default/public branch
	InstallDir   string
	Username     string
	Password     string
	SteamCmdPath string
}

// AppUpdate installs or updates a Steam app via SteamCMD, streaming progress.
func AppUpdate(opts AppUpdateOptions, progressChan chan<- ProgressUpdate) error {
	exeName := opts.SteamCmdPath
	if exeName == "" {
		exeName = "steamcmd"
	}

	args := []string{
		"+force_install_dir", opts.InstallDir,
		"+login", opts.Username, opts.Password,
		"+app_update", opts.AppID,
	}
	if opts.SteamBranch != "" {
		args = append(args, "-beta", opts.SteamBranch)
	}
	args = append(args, "validate", "+quit")

	log.Printf("[STEAMCMD] command: %s %s", exeName, formatSteamCmdArgs(args))

	cmd := exec.Command(exeName, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start steamcmd: %w", err)
	}

	failMsg, success := parseAppUpdateOutput(stdout, stderr, progressChan)
	waitErr := cmd.Wait()

	if failMsg != "" {
		if progressChan != nil {
			progressChan <- ProgressUpdate{Stage: "error", Message: failMsg}
		}
		return fmt.Errorf("steamcmd app update failed: %s", failMsg)
	}

	// SteamCMD often exits non-zero even after a successful install.
	installed := dedicatedServerInstalled(opts.InstallDir)
	if waitErr != nil && !success && !installed {
		if progressChan != nil {
			progressChan <- ProgressUpdate{Stage: "error", Message: waitErr.Error()}
		}
		return fmt.Errorf("steamcmd exited with error: %w", waitErr)
	}
	if waitErr != nil && (success || installed) {
		log.Printf("[STEAMCMD] app_update finished (exit %v) — treating as success", waitErr)
	}

	if !installed {
		abs, _ := filepath.Abs(opts.InstallDir)
		msg := fmt.Sprintf("install finished but no server binary found in %s", abs)
		if alt := FindSteamCmdDedicatedServerDir(opts.SteamCmdPath); alt != "" {
			msg += fmt.Sprintf("; check SteamCMD library at %s", alt)
		}
		if progressChan != nil {
			progressChan <- ProgressUpdate{Stage: "error", Message: msg}
		}
		return fmt.Errorf(msg)
	}

	if progressChan != nil {
		progressChan <- ProgressUpdate{Stage: "complete", Message: "Install complete", Percentage: 100}
	}
	return nil
}

func parseAppUpdateOutput(stdout, stderr io.Reader, progressChan chan<- ProgressUpdate) (failMsg string, success bool) {
	scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	send := func(u ProgressUpdate) {
		if progressChan != nil {
			progressChan <- u
		}
	}

	var fail string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		log.Printf("[STEAMCMD] %s", line)

		switch {
		case strings.Contains(line, "Logging in user"):
			send(ProgressUpdate{Stage: "logging_in", Message: "Logging into Steam...", Percentage: 5})
		case strings.Contains(line, "Connecting anonymously") || strings.Contains(line, "Waiting for user info"):
			send(ProgressUpdate{Stage: "connecting", Message: "Connecting to Steam...", Percentage: 10})
		case strings.Contains(line, "Update state") && strings.Contains(line, "reconfiguring"):
			send(ProgressUpdate{Stage: "downloading", Message: "Reconfiguring...", Percentage: 20})
		case strings.Contains(line, "Update state") && strings.Contains(line, "downloading"):
			send(ProgressUpdate{Stage: "downloading", Message: "Downloading...", Percentage: 50})
		case strings.Contains(line, "Update state") && strings.Contains(line, "validating"):
			send(ProgressUpdate{Stage: "downloading", Message: "Validating...", Percentage: 80})
		case strings.Contains(line, "Success! App '"):
			success = true
			send(ProgressUpdate{Stage: "downloading", Message: "Finalizing...", Percentage: 95})
		case strings.Contains(line, "ERROR!"):
			fail = line
		case strings.Contains(line, "FAILED login") || strings.Contains(line, "Login Failure"):
			fail = line
		}
	}
	return fail, success
}

var dedicatedServerBinaryNames = []string{
	"arma3server_x64",
	"arma3server_x64.exe",
	"arma3server",
	"arma3server.exe",
}

// dedicatedServerInstalled reports whether a SteamCMD install dir contains the server binary.
func dedicatedServerInstalled(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	if findDedicatedServerBinary(abs) != "" {
		return true
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if findDedicatedServerBinary(filepath.Join(abs, e.Name())) != "" {
			return true
		}
	}
	return false
}

func findDedicatedServerBinary(dir string) string {
	for _, name := range dedicatedServerBinaryNames {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func formatSteamCmdArgs(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		if i > 0 && args[i-1] == "+login" {
			parts[i] = "***"
			continue
		}
		if i > 1 && args[i-2] == "+login" {
			parts[i] = "***"
			continue
		}
		parts[i] = a
	}
	return strings.Join(parts, " ")
}

// FindSteamCmdDedicatedServerDir returns the most likely SteamCMD steamapps/common
// install folder when force_install_dir did not receive the game files.
func FindSteamCmdDedicatedServerDir(steamCmdPath string) string {
	var roots []string
	if steamCmdPath != "" && (filepath.IsAbs(steamCmdPath) || strings.Contains(steamCmdPath, string(os.PathSeparator))) {
		roots = append(roots, filepath.Join(filepath.Dir(steamCmdPath), "steamapps", "common"))
	}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(root, e.Name())
			if findDedicatedServerBinary(dir) != "" {
				return dir
			}
		}
	}
	return ""
}
