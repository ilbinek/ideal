// Command gosteam downloads Steam Workshop mods and full applications over a
// single login, with delta patching and pruning of removed files.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/tbdevent/gosteam/pkg/client"
	"github.com/tbdevent/gosteam/pkg/downloader"
	"github.com/tbdevent/gosteam/pkg/state"
	"github.com/tbdevent/gosteam/pkg/workshop"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "download":
		err = cmdDownload(ctx, os.Args[2:])
	case "app-download":
		err = cmdAppDownload(ctx, os.Args[2:])
	case "login":
		err = cmdLogin(ctx, os.Args[2:])
	case "list":
		err = cmdList(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `gosteam - Steam Workshop and application downloader

Usage:
  gosteam download      [flags] <pubfileid|collectionid> [...]
  gosteam app-download  [flags]
  gosteam login         [flags]
  gosteam list          [flags]

Run "gosteam <command> -h" for command flags.
`)
}

// authFlags are shared by commands that log in.
type authFlags struct {
	user      string
	password  string
	anonymous bool
	qr        bool
	stateDir  string
}

func (a *authFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&a.user, "user", "", "Steam account name")
	fs.StringVar(&a.password, "password", "", "Steam password (omit to be prompted; not needed if a token is cached)")
	fs.BoolVar(&a.anonymous, "anonymous", false, "log in anonymously (most workshop content is not available anonymously)")
	fs.BoolVar(&a.qr, "qr", false, "log in by scanning a QR code with the Steam mobile app")
	fs.StringVar(&a.stateDir, "state", ".gosteam", "directory for tokens, manifest cache, and install state")
}

func (a *authFlags) login(ctx context.Context, store *state.Store) (*client.Session, error) {
	opts := client.Options{
		Username:       a.user,
		Password:       a.password,
		Anonymous:      a.anonymous,
		UseQR:          a.qr,
		Store:          store,
		Guard:          promptGuard,
		OnChallengeURL: func(u string) { fmt.Println("Scan this QR challenge URL with the Steam mobile app:\n ", u) },
	}
	if !a.anonymous && !a.qr && a.user != "" && a.password == "" {
		if _, ok := store.Token(a.user); !ok {
			opts.Password = prompt("Password: ")
		}
	}
	return client.Login(ctx, opts)
}

func cmdDownload(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("download", flag.ExitOnError)
	var af authFlags
	af.register(fs)
	app := fs.Uint("app", uint(workshop.Arma3AppID), "Steam AppID")
	out := fs.String("out", "mods", "output directory for downloaded mods")
	maxDownloads := fs.Int("max-downloads", 8, "max concurrent chunk downloads")
	maxMods := fs.Int("max-mods", 4, "max mods downloaded concurrently")
	verifyAll := fs.Bool("verify-all", false, "re-validate every chunk even if the file hash matches")
	noPrune := fs.Bool("no-prune", false, "do not delete files removed on the remote")
	fs.Parse(args)

	ids, err := parseIDs(fs.Args())
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("no published file ids given")
	}

	store, err := state.Open(af.stateDir)
	if err != nil {
		return err
	}
	sess, err := af.login(ctx, store)
	if err != nil {
		return err
	}
	defer sess.Close()
	fmt.Println("Logged in. Resolving", len(ids), "item(s)...")

	ui := newProgressUI()
	d, err := downloader.New(ctx, sess, store, downloader.Config{
		AppID:             uint32(*app),
		OutDir:            *out,
		MaxDownloads:      *maxDownloads,
		MaxConcurrentMods: *maxMods,
		VerifyAll:         *verifyAll,
		Prune:             !*noPrune,
		OnProgress:        ui.handle,
	})
	if err != nil {
		return err
	}

	items, err := d.Resolve(ctx, ids)
	if err != nil {
		return err
	}
	fmt.Printf("Resolved %d item(s). Downloading...\n", len(items))
	ui.expected = len(items)

	results := d.DownloadItems(ctx, items)
	ui.finish()

	var failed int
	var totalDown, totalReuse uint64
	for _, r := range results {
		if r.Err != nil {
			failed++
			continue
		}
		totalDown += r.Stats.DownloadedBytes
		totalReuse += r.Stats.ReusedBytes
	}
	fmt.Printf("Done. %d ok, %d failed. Downloaded %s, reused %s.\n",
		len(results)-failed, failed, humanBytes(totalDown), humanBytes(totalReuse))
	if failed > 0 {
		return fmt.Errorf("%d item(s) failed", failed)
	}
	return nil
}

func cmdAppDownload(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("app-download", flag.ExitOnError)
	var af authFlags
	af.register(fs)
	appID := fs.Uint("app", 0, "Steam AppID to download (required)")
	branch := fs.String("branch", "public", "Steam branch / beta name")
	branchPassword := fs.String("branch-password", "", "password for a protected beta branch")
	out := fs.String("out", "app", "install directory for the application")
	osName := fs.String("os", "", "target OS: windows, linux, or macos (default: current OS)")
	arch := fs.String("arch", "64", "target architecture: 32 or 64")
	language := fs.String("language", "english", "language for language-specific depots")
	allPlatforms := fs.Bool("all-platforms", false, "download depots for all operating systems")
	allArchs := fs.Bool("all-archs", false, "download depots for all architectures")
	allLanguages := fs.Bool("all-languages", false, "download all language-specific depots")
	maxDownloads := fs.Int("max-downloads", 8, "max concurrent chunk downloads")
	maxDepots := fs.Int("max-depots", 2, "max depots downloaded concurrently")
	verifyAll := fs.Bool("verify-all", false, "re-validate every chunk even if the file hash matches")
	noPrune := fs.Bool("no-prune", false, "do not delete files removed on the remote")
	depotFlag := fs.String("depot", "", "comma-separated depot ids (optional; default: auto-select by OS/arch/language)")
	manifestFlag := fs.String("manifest", "", "comma-separated manifest ids (requires matching --depot)")
	fs.Parse(args)

	if *appID == 0 {
		return fmt.Errorf("--app is required")
	}

	depotIDs, err := parseUint32List(*depotFlag)
	if err != nil {
		return fmt.Errorf("invalid --depot: %w", err)
	}
	manifestIDs, err := parseUint64List(*manifestFlag)
	if err != nil {
		return fmt.Errorf("invalid --manifest: %w", err)
	}
	if len(manifestIDs) > 0 && len(manifestIDs) != len(depotIDs) {
		return fmt.Errorf("--manifest requires one id for every --depot")
	}

	store, err := state.Open(af.stateDir)
	if err != nil {
		return err
	}
	sess, err := af.login(ctx, store)
	if err != nil {
		return err
	}
	defer sess.Close()
	fmt.Printf("Logged in. Resolving app %d (branch %q)...\n", *appID, *branch)

	ui := newProgressUI()
	d, err := downloader.NewApp(ctx, sess, store, downloader.AppConfig{
		AppID:             uint32(*appID),
		Branch:            *branch,
		BranchPassword:    *branchPassword,
		OutDir:            *out,
		OS:                *osName,
		Arch:              *arch,
		Language:          *language,
		AllPlatforms:      *allPlatforms,
		AllArchs:          *allArchs,
		AllLanguages:      *allLanguages,
		DepotIDs:          depotIDs,
		ManifestIDs:       manifestIDs,
		MaxDownloads:      *maxDownloads,
		MaxConcurrentDeps: *maxDepots,
		VerifyAll:         *verifyAll,
		Prune:             !*noPrune,
		OnProgress:        ui.handleApp,
	})
	if err != nil {
		return err
	}

	depots, err := d.Resolve(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Resolved %d depot(s). Downloading into %s...\n", len(depots), *out)
	ui.expected = len(depots)

	results := d.DownloadDepots(ctx, depots)
	ui.finish()

	var failed int
	var totalDown, totalReuse uint64
	for _, r := range results {
		if r.Err != nil {
			failed++
			continue
		}
		totalDown += r.Stats.DownloadedBytes
		totalReuse += r.Stats.ReusedBytes
	}
	fmt.Printf("Done. %d ok, %d failed. Downloaded %s, reused %s.\n",
		len(results)-failed, failed, humanBytes(totalDown), humanBytes(totalReuse))
	if failed > 0 {
		return fmt.Errorf("%d depot(s) failed", failed)
	}
	return nil
}

func cmdLogin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	var af authFlags
	af.register(fs)
	fs.Parse(args)

	store, err := state.Open(af.stateDir)
	if err != nil {
		return err
	}
	sess, err := af.login(ctx, store)
	if err != nil {
		return err
	}
	defer sess.Close()
	fmt.Println("Login succeeded; refresh token cached in", af.stateDir)
	return nil
}

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	stateDir := fs.String("state", ".gosteam", "state directory")
	fs.Parse(args)

	store, err := state.Open(*stateDir)
	if err != nil {
		return err
	}
	installed := store.AllInstalled()
	apps := store.AllInstalledApps()
	if len(installed) == 0 && len(apps) == 0 {
		fmt.Println("No installed items recorded.")
		return nil
	}
	if len(installed) > 0 {
		fmt.Printf("%-14s %s\n", "WORKSHOP", "MANIFEST")
		for item, mid := range installed {
			fmt.Printf("%-14d %d\n", item, mid)
		}
	}
	if len(apps) > 0 {
		if len(installed) > 0 {
			fmt.Println()
		}
		fmt.Printf("%-18s %s\n", "APP:DEPOT", "MANIFEST")
		for key, mid := range apps {
			fmt.Printf("%-18s %d\n", key, mid)
		}
	}
	return nil
}

// parseIDs converts string args to published-file ids.
func parseIDs(args []string) ([]uint64, error) {
	ids := make([]uint64, 0, len(args))
	for _, a := range args {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		id, err := strconv.ParseUint(a, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid published file id %q", a)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func parseUint32List(s string) ([]uint32, error) {
	parts := splitCSV(s)
	if len(parts) == 0 {
		return nil, nil
	}
	out := make([]uint32, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid id %q", p)
		}
		out = append(out, uint32(n))
	}
	return out, nil
}

func parseUint64List(s string) ([]uint64, error) {
	parts := splitCSV(s)
	if len(parts) == 0 {
		return nil, nil
	}
	out := make([]uint64, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid id %q", p)
		}
		out = append(out, n)
	}
	return out, nil
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	raw := strings.Split(s, ",")
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func promptGuard(kind client.GuardKind) (string, error) {
	return prompt(fmt.Sprintf("Enter Steam Guard %s: ", kind)), nil
}

func prompt(label string) string {
	fmt.Print(label)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
