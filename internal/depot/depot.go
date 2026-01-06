package depot

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	ArmaAppID     = "107410"
	WorkshopDepot = "107410"
)

// ProgressUpdate represents a download progress update
type ProgressUpdate struct {
	Stage      string  // "connecting", "logging_in", "downloading", "complete", "error"
	Message    string  // Human-readable message
	Percentage float64 // Download percentage (0-100)
	FileName   string  // Current file being downloaded
	Error      error   // Error if any
}

// DownloadMod downloads a mod using DepotDownloader
func DownloadMod(workshopID, username, password, outputDir string) (string, error) {
	return DownloadModWithProgress(workshopID, username, password, outputDir, nil)
}

// DownloadModWithProgress downloads a mod using DepotDownloader and sends progress updates
func DownloadModWithProgress(workshopID, username, password, outputDir string, progressChan chan<- ProgressUpdate) (string, error) {
	log.Printf("[DEBUG] Starting download for workshop ID: %s", workshopID)
	// Create output directory for this mod
	modPath := filepath.Join(outputDir, workshopID)

	// Delete existing mod directory if it exists
	if err := os.RemoveAll(modPath); err != nil {
		return "", fmt.Errorf("failed to remove existing mod directory: %w", err)
	}

	// Create fresh directory
	if err := os.MkdirAll(modPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create mod directory: %w", err)
	}

	// Run DepotDownloader
	cmd := exec.Command(
		"DepotDownloader",
		"-app", ArmaAppID,
		"-pubfile", workshopID,
		"-username", username,
		"-password", password,
		"-dir", modPath,
	)

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the command
	log.Printf("[DEBUG] Starting DepotDownloader command for workshop ID: %s", workshopID)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start DepotDownloader: %w", err)
	}

	// Parse output if progress channel is provided
	if progressChan != nil {
		log.Printf("[DEBUG] Starting output parser with progress channel")
		go parseDepotOutput(stdout, stderr, progressChan)
	} else {
		// If no progress channel, just copy to stdout/stderr
		go io.Copy(os.Stdout, stdout)
		go io.Copy(os.Stderr, stderr)
	}

	// Wait for command to complete
	log.Printf("[DEBUG] Waiting for DepotDownloader to complete...")
	if err := cmd.Wait(); err != nil {
		log.Printf("[ERROR] DepotDownloader failed: %v", err)
		if progressChan != nil {
			progressChan <- ProgressUpdate{
				Stage:   "error",
				Message: "DepotDownloader failed",
				Error:   err,
			}
		}
		return "", fmt.Errorf("DepotDownloader failed: %w", err)
	}

	log.Printf("[DEBUG] DepotDownloader completed successfully")
	if progressChan != nil {
		progressChan <- ProgressUpdate{
			Stage:      "complete",
			Message:    "Download complete",
			Percentage: 100,
		}
	}

	return modPath, nil
}

// parseDepotOutput parses DepotDownloader output and sends progress updates
func parseDepotOutput(stdout, stderr io.Reader, progressChan chan<- ProgressUpdate) {
	log.Printf("[DEBUG] Parser started")
	// Regex patterns for parsing
	percentRegex := regexp.MustCompile(`^\s*(\d+\.\d+)%\s+(.+)$`)

	// Combine stdout and stderr
	multiReader := io.MultiReader(stdout, stderr)
	scanner := bufio.NewScanner(multiReader)

	for scanner.Scan() {
		line := scanner.Text()
		log.Printf("[DEPOT] %s", line)

		// Check for connection messages
		if strings.Contains(line, "Connecting to Steam3") {
			progressChan <- ProgressUpdate{
				Stage:   "connecting",
				Message: "Connecting to Steam...",
			}
		} else if strings.Contains(line, "Logging") && strings.Contains(line, "into Steam3") {
			progressChan <- ProgressUpdate{
				Stage:   "logging_in",
				Message: "Logging into Steam...",
			}
		} else if strings.Contains(line, "Downloading depot") {
			progressChan <- ProgressUpdate{
				Stage:   "downloading",
				Message: "Starting download...",
			}
		} else if strings.HasPrefix(line, "Pre-allocating") {
			// Extract filename from "Pre-allocating mods\@123\file.pbo"
			parts := strings.SplitN(line, " ", 2)
			if len(parts) == 2 {
				fileName := filepath.Base(parts[1])
				progressChan <- ProgressUpdate{
					Stage:    "downloading",
					Message:  "Pre-allocating files...",
					FileName: fileName,
				}
			}
		} else if match := percentRegex.FindStringSubmatch(line); match != nil {
			// Parse percentage and filename
			var percentage float64
			fmt.Sscanf(match[1], "%f", &percentage)
			fileName := strings.TrimSpace(match[2])
			fileName = filepath.Base(fileName)

			// Check if we've reached 100%
			if percentage >= 100.0 {
				progressChan <- ProgressUpdate{
					Stage:      "complete",
					Message:    "Download complete",
					Percentage: 100,
					FileName:   fileName,
				}
			} else {
				progressChan <- ProgressUpdate{
					Stage:      "downloading",
					Message:    fmt.Sprintf("Downloading: %.2f%%", percentage),
					Percentage: percentage,
					FileName:   fileName,
				}
			}
		} else if strings.Contains(line, "Total downloaded:") {
			progressChan <- ProgressUpdate{
				Stage:      "complete",
				Message:    "Download complete",
				Percentage: 100,
			}
		} else if strings.Contains(line, "Depot") && strings.Contains(line, "Downloaded") {
			progressChan <- ProgressUpdate{
				Stage:   "downloading",
				Message: line,
			}
		}
	}
}
