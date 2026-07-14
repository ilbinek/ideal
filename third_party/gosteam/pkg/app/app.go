// Package app resolves Steam applications into depot + manifest coordinates
// for a chosen branch (including password-protected betas), mirroring
// DepotDownloader's DownloadAppAsync discovery path.
package app

import (
	"context"
	"encoding/hex"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"

	steam "github.com/0xAozora/go-steam"
	"github.com/0xAozora/go-steam/protocol/protobuf"
	"github.com/0xAozora/go-steam/protocol/steamlang"
	"google.golang.org/protobuf/proto"

	"github.com/tbdevent/gosteam/pkg/steamrpc"
	"github.com/tbdevent/gosteam/pkg/vdf"
)

// DefaultBranch is Steam's public branch name.
const DefaultBranch = "public"

// Depot is a resolved app depot ready to download.
type Depot struct {
	AppID      uint32
	DepotID    uint32
	ManifestID uint64
	Branch     string // branch used for the manifest request code
	Name       string // optional human label (app name + depot id)
}

// ResolveOptions controls which depots are selected for an app download.
type ResolveOptions struct {
	AppID          uint32
	Branch         string // empty => "public"
	BranchPassword string // required for password-protected betas
	OS             string // windows|linux|macos; empty => current OS
	Arch           string // "32" or "64"; empty => "64"
	Language       string // empty => "english"
	AllPlatforms   bool
	AllArchs       bool
	AllLanguages   bool
	LowViolence    bool
	// DepotIDs, if non-empty, restricts resolution to these depot ids
	// (manifests still resolved from the branch unless ManifestIDs is set).
	DepotIDs []uint32
	// ManifestIDs, if non-empty, must align 1:1 with DepotIDs and skips
	// branch manifest lookup for those depots.
	ManifestIDs []uint64
}

// Resolver turns an app id + branch into downloadable Depots.
type Resolver struct {
	sc *steam.Client

	mu        sync.Mutex
	appTokens map[uint32]uint64
	appInfo   map[uint32]map[string]any // parsed appinfo trees
	// betaPasswords maps branch name -> raw password bytes from CheckAppBetaPassword.
	betaPasswords map[string][]byte
	// privateDepots caches private beta depot sections keyed by "appID:branch".
	privateDepots map[string]map[string]any
}

// NewResolver creates a resolver bound to a logged-in Steam client.
func NewResolver(sc *steam.Client) *Resolver {
	return &Resolver{
		sc:            sc,
		appTokens:     map[uint32]uint64{},
		appInfo:       map[uint32]map[string]any{},
		betaPasswords: map[string][]byte{},
		privateDepots: map[string]map[string]any{},
	}
}

// Resolve returns the depots to download for the given options.
func (r *Resolver) Resolve(ctx context.Context, opts ResolveOptions) ([]Depot, error) {
	if opts.AppID == 0 {
		return nil, fmt.Errorf("app: app id is required")
	}
	branch := opts.Branch
	if branch == "" {
		branch = DefaultBranch
	}
	osName := opts.OS
	if osName == "" {
		osName = steamOS()
	}
	arch := opts.Arch
	if arch == "" {
		arch = "64"
	}
	lang := opts.Language
	if lang == "" {
		lang = "english"
	}

	if err := r.requestAppInfo(ctx, opts.AppID); err != nil {
		return nil, err
	}
	tree := r.appInfo[opts.AppID]
	if tree == nil {
		return nil, fmt.Errorf("app: no product info for app %d", opts.AppID)
	}

	appName := appDisplayName(tree)
	depotsRoot := findDepotsRoot(tree)
	if depotsRoot == nil {
		return nil, fmt.Errorf("app: app %d has no depots section", opts.AppID)
	}

	var wanted map[uint32]uint64 // depotID -> explicit manifest, or 0 to resolve
	if len(opts.DepotIDs) > 0 {
		if len(opts.ManifestIDs) > 0 && len(opts.ManifestIDs) != len(opts.DepotIDs) {
			return nil, fmt.Errorf("app: --manifest requires one id per --depot")
		}
		wanted = make(map[uint32]uint64, len(opts.DepotIDs))
		for i, id := range opts.DepotIDs {
			var mid uint64
			if i < len(opts.ManifestIDs) {
				mid = opts.ManifestIDs[i]
			}
			wanted[id] = mid
		}
	}

	var out []Depot
	for key, val := range depotsRoot {
		depotID, err := strconv.ParseUint(key, 10, 32)
		if err != nil {
			continue // skip workshopdepot, branches, etc.
		}
		section, ok := val.(map[string]any)
		if !ok || len(section) == 0 {
			continue
		}
		id := uint32(depotID)

		if wanted != nil {
			if _, ok := wanted[id]; !ok {
				continue
			}
		} else if !depotMatchesFilters(section, osName, arch, lang, opts) {
			continue
		}

		manifestID := uint64(0)
		usedBranch := branch
		if wanted != nil {
			manifestID = wanted[id]
		}
		if manifestID == 0 {
			var resolveErr error
			manifestID, usedBranch, resolveErr = r.resolveManifest(ctx, opts.AppID, id, branch, opts.BranchPassword, map[uint32]bool{})
			if resolveErr != nil {
				return nil, resolveErr
			}
			if manifestID == 0 {
				continue
			}
		}

		name := appName
		if name == "" {
			name = fmt.Sprintf("App %d", opts.AppID)
		}
		out = append(out, Depot{
			AppID:      opts.AppID,
			DepotID:    id,
			ManifestID: manifestID,
			Branch:     usedBranch,
			Name:       fmt.Sprintf("%s (depot %d)", name, id),
		})
	}

	if wanted != nil {
		found := make(map[uint32]bool, len(out))
		for _, d := range out {
			found[d.DepotID] = true
		}
		for id := range wanted {
			if !found[id] {
				return nil, fmt.Errorf("app: depot %d not listed for app %d", id, opts.AppID)
			}
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("app: no depots to download for app %d (branch %q)", opts.AppID, branch)
	}
	return out, nil
}

// BranchPasswordHash returns the hex-encoded beta password for branch, if known
// from a prior CheckAppBetaPassword (for CDN manifest request codes).
func (r *Resolver) BranchPasswordHash(branch string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pw, ok := r.betaPasswords[branch]; ok && len(pw) > 0 {
		return hex.EncodeToString(pw)
	}
	return ""
}

func steamOS() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "macos"
	default:
		return "linux"
	}
}

func appDisplayName(tree map[string]any) string {
	root := unwrapAppinfo(tree)
	if common := vdf.Child(root, "common"); common != nil {
		if name, ok := vdf.String(common, "name"); ok {
			return name
		}
	}
	return ""
}

func unwrapAppinfo(tree map[string]any) map[string]any {
	if child := vdf.Child(tree, "appinfo"); child != nil {
		return child
	}
	return tree
}

func findDepotsRoot(tree map[string]any) map[string]any {
	root := unwrapAppinfo(tree)
	if d := vdf.Child(root, "depots"); d != nil {
		return d
	}
	return vdf.Child(tree, "depots")
}

func depotMatchesFilters(section map[string]any, osName, arch, lang string, opts ResolveOptions) bool {
	cfg := vdf.Child(section, "config")
	if cfg == nil {
		return true
	}
	if !opts.AllPlatforms {
		if oslist, ok := vdf.String(cfg, "oslist"); ok && strings.TrimSpace(oslist) != "" {
			okOS := false
			for _, part := range strings.Split(oslist, ",") {
				if strings.EqualFold(strings.TrimSpace(part), osName) {
					okOS = true
					break
				}
			}
			if !okOS {
				return false
			}
		}
	}
	if !opts.AllArchs {
		if osarch, ok := vdf.String(cfg, "osarch"); ok && strings.TrimSpace(osarch) != "" {
			if !strings.EqualFold(strings.TrimSpace(osarch), arch) {
				return false
			}
		}
	}
	if !opts.AllLanguages {
		if depotLang, ok := vdf.String(cfg, "language"); ok && strings.TrimSpace(depotLang) != "" {
			if !strings.EqualFold(strings.TrimSpace(depotLang), lang) {
				return false
			}
		}
	}
	if !opts.LowViolence && vdf.Bool(cfg, "lowviolence") {
		return false
	}
	return true
}

func (r *Resolver) resolveManifest(ctx context.Context, appID, depotID uint32, branch, branchPassword string, seen map[uint32]bool) (uint64, string, error) {
	if seen[appID] {
		return 0, branch, fmt.Errorf("app: depotfromapp cycle involving app %d", appID)
	}
	seen[appID] = true

	if err := r.requestAppInfo(ctx, appID); err != nil {
		return 0, branch, err
	}

	depots := findDepotsRoot(r.appInfo[appID])
	if depots == nil {
		return 0, branch, nil
	}
	section := vdf.Child(depots, strconv.FormatUint(uint64(depotID), 10))
	if section == nil {
		return 0, branch, nil
	}

	manifests := vdf.Child(section, "manifests")
	if manifests == nil {
		if fromApp, ok := vdf.Uint32(section, "depotfromapp"); ok && fromApp != 0 {
			if fromApp == appID {
				return 0, branch, fmt.Errorf("app: depot %d has depotfromapp pointing at itself", depotID)
			}
			return r.resolveManifest(ctx, fromApp, depotID, branch, branchPassword, seen)
		}
		return 0, branch, nil
	}

	if gid := manifestGID(manifests, branch); gid != 0 {
		return gid, branch, nil
	}

	// Password-protected / private beta path.
	if !strings.EqualFold(branch, DefaultBranch) && branchPassword != "" {
		priv, err := r.privateDepotSection(ctx, appID, branch, branchPassword)
		if err != nil {
			return 0, branch, err
		}
		if priv != nil {
			if depotSec := vdf.Child(priv, strconv.FormatUint(uint64(depotID), 10)); depotSec != nil {
				if m := vdf.Child(depotSec, "manifests"); m != nil {
					if gid := manifestGID(m, branch); gid != 0 {
						return gid, branch, nil
					}
				}
			}
		}
	}

	if !strings.EqualFold(branch, DefaultBranch) {
		if gid := manifestGID(manifests, DefaultBranch); gid != 0 {
			return gid, DefaultBranch, nil
		}
	}
	return 0, branch, nil
}

func manifestGID(manifests map[string]any, branch string) uint64 {
	node := vdf.Child(manifests, branch)
	if node == nil {
		return 0
	}
	if gid, ok := vdf.Uint64(node, "gid"); ok {
		return gid
	}
	// Some older appinfo stores the gid as the branch value itself.
	if gid, ok := vdf.Uint64(manifests, branch); ok {
		return gid
	}
	return 0
}

func (r *Resolver) requestAppInfo(ctx context.Context, appID uint32) error {
	r.mu.Lock()
	if _, ok := r.appInfo[appID]; ok {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	token, err := r.picsAccessToken(ctx, appID)
	if err != nil {
		return err
	}

	appReq := &protobuf.CMsgClientPICSProductInfoRequest_AppInfo{Appid: proto.Uint32(appID)}
	if token != 0 {
		appReq.AccessToken = proto.Uint64(token)
	}
	req := &protobuf.CMsgClientPICSProductInfoRequest{
		Apps: []*protobuf.CMsgClientPICSProductInfoRequest_AppInfo{appReq},
	}
	resp := &protobuf.CMsgClientPICSProductInfoResponse{}
	if err := steamrpc.Call(ctx, r.sc, steamlang.EMsg_ClientPICSProductInfoRequest, "", req, resp); err != nil {
		return fmt.Errorf("app: product info for %d: %w", appID, err)
	}

	for _, app := range resp.GetApps() {
		if app.GetAppid() != appID || len(app.GetBuffer()) == 0 {
			continue
		}
		buf := app.GetBuffer()
		if buf[len(buf)-1] == 0 {
			buf = buf[:len(buf)-1]
		}
		tree, err := vdf.ParseText(buf)
		if err != nil {
			return fmt.Errorf("app: parse appinfo for %d: %w", appID, err)
		}
		r.mu.Lock()
		r.appInfo[appID] = tree
		r.mu.Unlock()
		return nil
	}
	return fmt.Errorf("app: empty product info for app %d (no access?)", appID)
}

func (r *Resolver) picsAccessToken(ctx context.Context, appID uint32) (uint64, error) {
	r.mu.Lock()
	if t, ok := r.appTokens[appID]; ok {
		r.mu.Unlock()
		return t, nil
	}
	r.mu.Unlock()

	req := &protobuf.CMsgClientPICSAccessTokenRequest{Appids: []uint32{appID}}
	resp := &protobuf.CMsgClientPICSAccessTokenResponse{}
	if err := steamrpc.Call(ctx, r.sc, steamlang.EMsg_ClientPICSAccessTokenRequest, "", req, resp); err != nil {
		return 0, fmt.Errorf("app: pics access token for %d: %w", appID, err)
	}
	for _, denied := range resp.GetAppDeniedTokens() {
		if denied == appID {
			// Still try product info without a token.
			r.mu.Lock()
			r.appTokens[appID] = 0
			r.mu.Unlock()
			return 0, nil
		}
	}
	var token uint64
	for _, t := range resp.GetAppAccessTokens() {
		if t.GetAppid() == appID {
			token = t.GetAccessToken()
			break
		}
	}
	r.mu.Lock()
	r.appTokens[appID] = token
	r.mu.Unlock()
	return token, nil
}

func (r *Resolver) checkAppBetaPassword(ctx context.Context, appID uint32, password string) error {
	r.mu.Lock()
	if len(r.betaPasswords) > 0 {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	req := &protobuf.CMsgClientCheckAppBetaPassword{
		AppId:        proto.Uint32(appID),
		Betapassword: proto.String(password),
	}
	resp := &protobuf.CMsgClientCheckAppBetaPasswordResponse{}
	if err := steamrpc.Call(ctx, r.sc, steamlang.EMsg_ClientCheckAppBetaPassword, "", req, resp); err != nil {
		return fmt.Errorf("app: check beta password: %w", err)
	}
	if steamlang.EResult(resp.GetEresult()) != steamlang.EResult_OK {
		return fmt.Errorf("app: check beta password failed: %v", steamlang.EResult(resp.GetEresult()))
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, bp := range resp.GetBetapasswords() {
		name := bp.GetBetaname()
		raw, err := hex.DecodeString(bp.GetBetapassword())
		if err != nil {
			// Steam sometimes returns raw bytes as a latin-1 string; use as-is.
			raw = []byte(bp.GetBetapassword())
		}
		r.betaPasswords[name] = raw
	}
	return nil
}

func (r *Resolver) privateDepotSection(ctx context.Context, appID uint32, branch, password string) (map[string]any, error) {
	cacheKey := fmt.Sprintf("%d:%s", appID, branch)
	r.mu.Lock()
	if sec, ok := r.privateDepots[cacheKey]; ok {
		r.mu.Unlock()
		return sec, nil
	}
	r.mu.Unlock()

	if err := r.checkAppBetaPassword(ctx, appID, password); err != nil {
		return nil, err
	}

	r.mu.Lock()
	pw, ok := r.betaPasswords[branch]
	r.mu.Unlock()
	if !ok || len(pw) == 0 {
		return nil, fmt.Errorf("app: password invalid for branch %q (or branch does not exist)", branch)
	}

	token, err := r.picsAccessToken(ctx, appID)
	if err != nil {
		return nil, err
	}

	req := &protobuf.CMsgClientPICSPrivateBetaRequest{
		Appid:        proto.Uint32(appID),
		AccessToken:  proto.Uint64(token),
		BetaName:     proto.String(branch),
		PasswordHash: pw,
	}
	resp := &protobuf.CMsgClientPICSPrivateBetaResponse{}
	if err := steamrpc.Call(ctx, r.sc, steamlang.EMsg_ClientPICSPrivateBetaRequest, "", req, resp); err != nil {
		return nil, fmt.Errorf("app: private beta for %d/%s: %w", appID, branch, err)
	}
	if steamlang.EResult(resp.GetEresult()) != steamlang.EResult_OK {
		return nil, fmt.Errorf("app: private beta for %d/%s failed: %v", appID, branch, steamlang.EResult(resp.GetEresult()))
	}
	buf := resp.GetDepotSection()
	if len(buf) == 0 {
		return nil, fmt.Errorf("app: private beta for %d/%s returned empty depot section", appID, branch)
	}
	if buf[len(buf)-1] == 0 {
		buf = buf[:len(buf)-1]
	}
	tree, err := vdf.ParseText(buf)
	if err != nil {
		// Private beta sections are sometimes binary KeyValues.
		tree, err = vdf.ParseBinary(buf)
		if err != nil {
			return nil, fmt.Errorf("app: parse private beta depot section: %w", err)
		}
	}
	// Unwrap if wrapped in a root key.
	if inner := vdf.Child(tree, "depots"); inner != nil {
		tree = inner
	}

	r.mu.Lock()
	r.privateDepots[cacheKey] = tree
	r.mu.Unlock()
	return tree, nil
}
