// Package cdn downloads and decrypts Steam depot manifests and content chunks.
// It builds on a logged-in go-steam client for the control-plane RPCs (server
// list, manifest request codes, depot keys, CDN auth tokens) and does the actual
// HTTP transfers, AES decryption, decompression (zstd/LZMA), and Adler-32
// verification itself. This is the Go counterpart of SteamKit2's CDN.Client plus
// DepotDownloader's CDNClientPool.
package cdn

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	steam "github.com/0xAozora/go-steam"
	"github.com/0xAozora/go-steam/protocol/protobuf"
	"github.com/0xAozora/go-steam/protocol/protobuf/unified"
	"github.com/0xAozora/go-steam/protocol/steamlang"

	"github.com/tbdevent/gosteam/pkg/crypto"
	"github.com/tbdevent/gosteam/pkg/manifest"
	"github.com/tbdevent/gosteam/pkg/steamrpc"
)

// maxFetchAttempts is how many different servers we try for one object before giving up.
const maxFetchAttempts = 5

// Client downloads manifests and chunks for a single logged-in session.
type Client struct {
	sc   *steam.Client
	pool *Pool
	http *http.Client

	keyMu    sync.Mutex
	depotKey map[uint32][]byte // depotID -> 32-byte key (per-depot, user-independent)
}

// New creates a CDN client, loading the content server list for cellID.
func New(ctx context.Context, sc *steam.Client, cellID uint32) (*Client, error) {
	pool, err := NewPool(ctx, sc, cellID)
	if err != nil {
		return nil, err
	}
	return &Client{
		sc:       sc,
		pool:     pool,
		http:     &http.Client{Timeout: 2 * time.Minute},
		depotKey: map[uint32][]byte{},
	}, nil
}

// DepotKey returns the (cached) AES-256 decryption key for a depot.
func (c *Client) DepotKey(ctx context.Context, appID, depotID uint32) ([]byte, error) {
	c.keyMu.Lock()
	if k, ok := c.depotKey[depotID]; ok {
		c.keyMu.Unlock()
		return k, nil
	}
	c.keyMu.Unlock()

	req := &protobuf.CMsgClientGetDepotDecryptionKey{AppId: &appID, DepotId: &depotID}
	resp := &protobuf.CMsgClientGetDepotDecryptionKeyResponse{}
	if err := steamrpc.Call(ctx, c.sc, steamlang.EMsg_ClientGetDepotDecryptionKey, "", req, resp); err != nil {
		return nil, fmt.Errorf("cdn: get depot key: %w", err)
	}
	if steamlang.EResult(resp.GetEresult()) != steamlang.EResult_OK {
		return nil, fmt.Errorf("cdn: depot %d key request failed: %v", depotID, steamlang.EResult(resp.GetEresult()))
	}
	key := resp.GetDepotEncryptionKey()
	if len(key) != 32 {
		return nil, fmt.Errorf("cdn: depot %d returned %d-byte key (want 32)", depotID, len(key))
	}

	c.keyMu.Lock()
	c.depotKey[depotID] = key
	c.keyMu.Unlock()
	return key, nil
}

// manifestRequestCode obtains a short-lived (~5 min) code authorizing a manifest fetch.
func (c *Client) manifestRequestCode(ctx context.Context, appID, depotID uint32, manifestID uint64) (uint64, error) {
	req := &unified.CContentServerDirectory_GetManifestRequestCode_Request{
		AppId:      &appID,
		DepotId:    &depotID,
		ManifestId: &manifestID,
	}
	resp := &unified.CContentServerDirectory_GetManifestRequestCode_Response{}
	if err := steamrpc.Call(ctx, c.sc, steamlang.EMsg_ServiceMethodCallFromClient, "ContentServerDirectory.GetManifestRequestCode#1", req, resp); err != nil {
		return 0, fmt.Errorf("cdn: manifest request code: %w", err)
	}
	code := resp.GetManifestRequestCode()
	if code == 0 {
		return 0, fmt.Errorf("cdn: depot %d manifest %d: no request code (no access?)", depotID, manifestID)
	}
	return code, nil
}

// DownloadManifest fetches and parses a depot manifest. depotKey is required if
// the manifest's filenames are encrypted; pass the result of DepotKey.
func (c *Client) DownloadManifest(ctx context.Context, appID, depotID uint32, manifestID uint64, depotKey []byte) (*manifest.Manifest, []byte, error) {
	code, err := c.manifestRequestCode(ctx, appID, depotID, manifestID)
	if err != nil {
		return nil, nil, err
	}
	path := fmt.Sprintf("/depot/%d/manifest/%d/5/%d", depotID, manifestID, code)
	raw, err := c.fetch(ctx, appID, depotID, path)
	if err != nil {
		return nil, nil, fmt.Errorf("cdn: download manifest %d: %w", manifestID, err)
	}
	m, err := manifest.Parse(raw, depotKey)
	if err != nil {
		return nil, nil, err
	}
	return m, raw, nil
}

// DownloadChunk fetches one chunk, decrypts it with depotKey, decompresses it,
// and verifies its length and Adler-32 checksum. The returned slice is the
// chunk's uncompressed content.
func (c *Client) DownloadChunk(ctx context.Context, appID, depotID uint32, chunk manifest.Chunk, depotKey []byte) ([]byte, error) {
	path := "/depot/" + fmt.Sprint(depotID) + "/chunk/" + hex.EncodeToString(chunk.ID)
	enc, err := c.fetch(ctx, appID, depotID, path)
	if err != nil {
		return nil, fmt.Errorf("cdn: download chunk %x: %w", chunk.ID, err)
	}

	dec, err := crypto.SymmetricDecrypt(depotKey, enc)
	if err != nil {
		return nil, fmt.Errorf("cdn: decrypt chunk %x: %w", chunk.ID, err)
	}
	out, err := decompressChunk(dec)
	if err != nil {
		return nil, fmt.Errorf("cdn: chunk %x: %w", chunk.ID, err)
	}
	if uint32(len(out)) != chunk.UncompLen {
		return nil, fmt.Errorf("cdn: chunk %x length %d != expected %d", chunk.ID, len(out), chunk.UncompLen)
	}
	if got := manifest.Adler32(out); got != chunk.CRC {
		return nil, fmt.Errorf("cdn: chunk %x checksum %#x != expected %#x (corrupt or wrong depot key)", chunk.ID, got, chunk.CRC)
	}
	return out, nil
}

// fetch GETs a CDN object, trying multiple servers on failure and attaching a
// CDN auth token if a server demands one (HTTP 401/403).
func (c *Client) fetch(ctx context.Context, appID, depotID uint32, path string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < maxFetchAttempts; attempt++ {
		srv := c.pool.Get()

		body, status, err := c.httpGet(ctx, srv.baseURL()+path)
		if err == nil && status == http.StatusOK {
			return body, nil
		}

		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			// Retry once against the same host with an auth token.
			if tok, terr := c.pool.Token(ctx, appID, depotID, srv.Host); terr == nil && tok != "" {
				body, status, err = c.httpGet(ctx, srv.baseURL()+path+tok)
				if err == nil && status == http.StatusOK {
					return body, nil
				}
			}
		}

		c.pool.Penalize(srv)
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("server %s returned HTTP %d", srv.Host, status)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

func (c *Client) httpGet(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "Valve/Steam HTTP Client 1.0")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, resp.StatusCode, nil
	}
	body, err := io.ReadAll(resp.Body)
	return body, resp.StatusCode, err
}
