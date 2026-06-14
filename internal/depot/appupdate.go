package depot

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os/exec"
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

	failMsg := parseAppUpdateOutput(stdout, stderr, progressChan)
	waitErr := cmd.Wait()

	if failMsg != "" {
		if progressChan != nil {
			progressChan <- ProgressUpdate{Stage: "error", Message: failMsg}
		}
		return fmt.Errorf("steamcmd app update failed: %s", failMsg)
	}
	if waitErr != nil {
		if progressChan != nil {
			progressChan <- ProgressUpdate{Stage: "error", Message: waitErr.Error()}
		}
		return fmt.Errorf("steamcmd exited with error: %w", waitErr)
	}

	if progressChan != nil {
		progressChan <- ProgressUpdate{Stage: "complete", Message: "Install complete", Percentage: 100}
	}
	return nil
}

func parseAppUpdateOutput(stdout, stderr io.Reader, progressChan chan<- ProgressUpdate) string {
	scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	send := func(u ProgressUpdate) {
		if progressChan != nil {
			progressChan <- u
		}
	}

	var failMsg string
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
			send(ProgressUpdate{Stage: "downloading", Message: "Finalizing...", Percentage: 95})
		case strings.Contains(line, "ERROR!"):
			failMsg = line
		case strings.Contains(line, "FAILED login") || strings.Contains(line, "Login Failure"):
			failMsg = line
		}
	}
	return failMsg
}
