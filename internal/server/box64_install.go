package server

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"arma3modmanager/internal/utils"
)

const (
	box64DebIndexURL = "https://raw.githubusercontent.com/ryanfortner/box64-debs/master/debian/Packages"
	box64DebBaseURL  = "https://github.com/ryanfortner/box64-debs/raw/master/debian/"
)

// ManagedBox64Dir is where Ideal stores a private Box64 binary (no root/apt).
func ManagedBox64Dir() string {
	return filepath.Join(utils.DataRoot(), "tools", "box64")
}

// ManagedBox64Path is the Ideal-owned box64 executable path.
func ManagedBox64Path() string {
	return filepath.Join(ManagedBox64Dir(), "box64")
}

func managedBox64VersionPath() string {
	return filepath.Join(ManagedBox64Dir(), "version.txt")
}

func managedBox64Available() (path, version string, ok bool) {
	path = ManagedBox64Path()
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return "", "", false
	}
	version = strings.TrimSpace(readFileTrim(managedBox64VersionPath()))
	if version == "" {
		version = probeVersion(path, "--version")
	}
	return path, version, true
}

func readFileTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

type box64DebInfo struct {
	Version  string
	Filename string
	SHA256   string
	Size     int64
}

// latestGenericBox64Deb picks the newest Package: box64 entry (generic arm64).
func latestGenericBox64Deb(index string) (*box64DebInfo, error) {
	var best *box64DebInfo
	var cur *box64DebInfo
	flush := func() {
		if cur == nil || cur.Filename == "" {
			cur = nil
			return
		}
		// Only the generic package name "box64", not box64-rpi5arm64 etc.
		best = cur
		cur = nil
	}

	sc := bufio.NewScanner(strings.NewReader(index))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		key, val, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		switch key {
		case "Package":
			flush()
			if val == "box64" {
				cur = &box64DebInfo{}
			}
		case "Version":
			if cur != nil {
				cur.Version = val
			}
		case "Filename":
			if cur != nil {
				cur.Filename = strings.TrimPrefix(val, "./")
			}
		case "SHA256":
			if cur != nil {
				cur.SHA256 = strings.ToLower(strings.TrimSpace(val))
			}
		case "Size":
			if cur != nil {
				var n int64
				fmt.Sscanf(val, "%d", &n)
				cur.Size = n
			}
		}
	}
	flush()
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if best == nil {
		return nil, fmt.Errorf("no generic box64 package found in index")
	}
	return best, nil
}

// InstallManagedBox64 downloads a prebuilt generic ARM64 Box64 .deb and extracts
// the binary into Ideal's data directory. Requires linux/arm64. No sudo.
func InstallManagedBox64(ctx context.Context) (path, version string, err error) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "arm64" {
		return "", "", fmt.Errorf("managed Box64 install is only supported on linux/arm64 (this host is %s/%s)", runtime.GOOS, runtime.GOARCH)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	client := &http.Client{Timeout: 3 * time.Minute}
	index, err := httpGetString(ctx, client, box64DebIndexURL)
	if err != nil {
		return "", "", fmt.Errorf("fetch box64 package index: %w", err)
	}
	info, err := latestGenericBox64Deb(index)
	if err != nil {
		return "", "", err
	}

	tmpDir, err := os.MkdirTemp("", "ideal-box64-*")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmpDir)

	debPath := filepath.Join(tmpDir, filepath.Base(info.Filename))
	debURL := box64DebBaseURL + info.Filename
	if err := httpDownloadFile(ctx, client, debURL, debPath); err != nil {
		return "", "", fmt.Errorf("download box64 deb: %w", err)
	}
	if info.SHA256 != "" {
		sum, err := fileSHA256(debPath)
		if err != nil {
			return "", "", err
		}
		if sum != info.SHA256 {
			return "", "", fmt.Errorf("box64 deb checksum mismatch: got %s want %s", sum, info.SHA256)
		}
	}

	binOut := filepath.Join(tmpDir, "box64")
	if err := extractBox64FromDeb(ctx, debPath, tmpDir, binOut); err != nil {
		return "", "", err
	}

	if err := os.MkdirAll(ManagedBox64Dir(), 0755); err != nil {
		return "", "", err
	}
	dest := ManagedBox64Path()
	if err := copyFileMode(binOut, dest, 0755); err != nil {
		return "", "", fmt.Errorf("install box64 binary: %w", err)
	}
	_ = os.WriteFile(managedBox64VersionPath(), []byte(info.Version+"\n"), 0644)

	ver := probeVersion(dest, "--version")
	if ver == "" {
		ver = info.Version
	}
	return dest, ver, nil
}

func httpGetString(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ideal-arma-modmanager")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func httpDownloadFile(ctx context.Context, client *http.Client, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ideal-arma-modmanager")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, io.LimitReader(resp.Body, 64<<20))
	return err
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFileMode(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// extractBox64FromDeb uses host ar/tar (standard on Linux) to pull usr/*/bin/box64.
func extractBox64FromDeb(ctx context.Context, debPath, workDir, destBin string) error {
	arBin, err := exec.LookPath("ar")
	if err != nil {
		return fmt.Errorf("ar not found (install binutils) to extract box64.deb: %w", err)
	}
	tarBin, err := exec.LookPath("tar")
	if err != nil {
		return fmt.Errorf("tar not found to extract box64.deb: %w", err)
	}

	cmd := exec.CommandContext(ctx, arBin, "x", debPath)
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ar x: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	var dataTar string
	for _, name := range []string{"data.tar.xz", "data.tar.zst", "data.tar.gz", "data.tar.bz2", "data.tar"} {
		p := filepath.Join(workDir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			dataTar = p
			break
		}
	}
	if dataTar == "" {
		return fmt.Errorf("data.tar.* not found inside box64.deb")
	}

	// List members and pick a box64 binary path.
	list := exec.CommandContext(ctx, tarBin, "-tf", dataTar)
	listOut, err := list.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tar -tf: %w (%s)", err, strings.TrimSpace(string(listOut)))
	}
	member := ""
	for _, line := range strings.Split(string(listOut), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "./")
		base := filepath.Base(line)
		if base != "box64" {
			continue
		}
		// Prefer usr/local/bin or usr/bin
		if strings.Contains(line, "/bin/box64") {
			member = line
			if strings.Contains(line, "usr/local/bin") || strings.HasSuffix(line, "usr/bin/box64") {
				break
			}
		}
	}
	if member == "" {
		return fmt.Errorf("box64 binary not found inside %s", filepath.Base(dataTar))
	}

	extract := exec.CommandContext(ctx, tarBin, "-xOf", dataTar, "./"+member)
	out, err := extract.Output()
	if err != nil {
		// Some tars want the member without ./
		extract = exec.CommandContext(ctx, tarBin, "-xOf", dataTar, member)
		out, err = extract.Output()
		if err != nil {
			return fmt.Errorf("tar extract %s: %w", member, err)
		}
	}
	if len(out) < 1024 {
		return fmt.Errorf("extracted box64 looks too small (%d bytes)", len(out))
	}
	return os.WriteFile(destBin, out, 0755)
}
