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

// downloadSteamCmd downloads a mod using SteamCMD.
//
// Unlike DepotDownloader, SteamCMD always writes workshop items to
// <steamcmd_dir>/steamapps/workshop/content/<appid>/<id> and ignores any target
// directory. We therefore parse the actual destination from SteamCMD's
// "Success. Downloaded item <id> to "<path>"" line and move it into
// OutputDir/<id> so the rest of the pipeline is unchanged.
func downloadSteamCmd(opts Options, progressChan chan<- ProgressUpdate) (string, error) {
	workshopID := opts.WorkshopID
	log.Printf("[DEBUG] Starting SteamCMD download for workshop ID: %s", workshopID)

	exeName := opts.SteamCmdPath
	if exeName == "" {
		exeName = "steamcmd"
	}

	// +login must precede +workshop_download_item.
	cmd := exec.Command(
		exeName,
		"+login", opts.Username, opts.Password,
		"+workshop_download_item", ArmaAppID, workshopID,
		"+quit",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start steamcmd: %w", err)
	}

	// Parse output synchronously: capture the downloaded path and emit stages.
	downloadedPath, failMsg := parseSteamCmdOutput(stdout, stderr, progressChan)

	// SteamCMD exit codes are unreliable, so success is judged by whether we
	// captured a destination path that actually exists on disk.
	waitErr := cmd.Wait()

	if downloadedPath == "" || !dirExists(downloadedPath) {
		msg := "SteamCMD did not report a downloaded item"
		if failMsg != "" {
			msg = failMsg
		} else if waitErr != nil {
			msg = fmt.Sprintf("steamcmd exited with error: %v", waitErr)
		}
		if progressChan != nil {
			progressChan <- ProgressUpdate{Stage: "error", Message: msg}
		}
		return "", fmt.Errorf("steamcmd download failed: %s", msg)
	}

	// Move the downloaded content into OutputDir/<id>.
	modPath := filepath.Join(opts.OutputDir, workshopID)
	if progressChan != nil {
		progressChan <- ProgressUpdate{Stage: "downloading", Message: "Moving files into place...", Percentage: 95}
	}
	if err := os.RemoveAll(modPath); err != nil {
		return "", fmt.Errorf("failed to remove existing mod directory: %w", err)
	}
	if err := moveDir(downloadedPath, modPath); err != nil {
		if progressChan != nil {
			progressChan <- ProgressUpdate{Stage: "error", Message: "Failed to move downloaded files: " + err.Error()}
		}
		return "", fmt.Errorf("failed to move steamcmd output: %w", err)
	}

	log.Printf("[DEBUG] SteamCMD download complete for %s -> %s", workshopID, modPath)
	if progressChan != nil {
		progressChan <- ProgressUpdate{Stage: "complete", Message: "Download complete", Percentage: 100}
	}
	return modPath, nil
}

// parseSteamCmdOutput scans SteamCMD output, emits coarse stage progress, and
// returns the captured download path (if any) and a failure message (if any).
func parseSteamCmdOutput(stdout, stderr io.Reader, progressChan chan<- ProgressUpdate) (downloadedPath, failMsg string) {
	scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	send := func(u ProgressUpdate) {
		if progressChan != nil {
			progressChan <- u
		}
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		log.Printf("[STEAMCMD] %s", line)

		switch {
		case strings.Contains(line, "Logging in user"):
			send(ProgressUpdate{Stage: "logging_in", Message: "Logging into Steam..."})
		case strings.Contains(line, "Connecting anonymously") || strings.Contains(line, "Waiting for user info"):
			send(ProgressUpdate{Stage: "connecting", Message: "Connecting to Steam..."})
		case strings.Contains(line, "Downloading item"):
			send(ProgressUpdate{Stage: "downloading", Message: "Downloading...", Percentage: 50})
		case strings.Contains(line, "Success. Downloaded item"):
			if p := extractQuotedPath(line); p != "" {
				downloadedPath = p
			}
		case strings.Contains(line, "ERROR! Download item") || (strings.Contains(line, "Download item") && strings.Contains(line, "failed")):
			failMsg = line
		case strings.Contains(line, "FAILED login") || strings.Contains(line, "Login Failure"):
			failMsg = line
		}
	}
	return downloadedPath, failMsg
}

// extractQuotedPath returns the text inside the first pair of double quotes,
// e.g. from: Success. Downloaded item 123 to "/path/to/123" (456 bytes)
func extractQuotedPath(line string) string {
	parts := strings.SplitN(line, "\"", 3)
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// moveDir moves src to dst, falling back to a recursive copy + remove when the
// two paths live on different filesystems (SteamCMD's content dir is frequently
// on a different volume than ./mods).
func moveDir(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Fallback: copy then remove the source.
	if err := copyDir(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
