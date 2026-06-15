// Package workshop resolves Steam Workshop published-file ids into the depot +
// manifest coordinates needed to download their content via the standard
// SteamPipe pipeline. Collections are expanded recursively. This mirrors
// DepotDownloader's ProcessPublishedFileAsync and its workshopdepot lookup.
package workshop

import (
	"context"
	"fmt"
	"sync"

	steam "github.com/0xAozora/go-steam"
	"github.com/0xAozora/go-steam/protocol/protobuf"
	"github.com/0xAozora/go-steam/protocol/protobuf/unified"
	"github.com/0xAozora/go-steam/protocol/steamlang"
	"google.golang.org/protobuf/proto"

	"github.com/tbdevent/gosteam/pkg/steamrpc"
	"github.com/tbdevent/gosteam/pkg/vdf"
)

// Arma3AppID is the Steam AppID for Arma 3.
const Arma3AppID uint32 = 107410

// fileTypeCollection is EWorkshopFileType_Collection.
const fileTypeCollection = 2

// Item is a resolved workshop item ready to download.
type Item struct {
	PubFileID  uint64
	AppID      uint32 // consumer app id (e.g. 107410)
	DepotID    uint32 // the app's workshop depot
	ManifestID uint64 // from hcontent_file
	Title      string
}

// Resolver turns published-file ids into downloadable Items using a logged-in client.
type Resolver struct {
	sc *steam.Client

	mu         sync.Mutex
	depotByApp map[uint32]uint32 // appID -> workshop depot id
}

// NewResolver creates a resolver bound to a session's underlying client.
func NewResolver(sc *steam.Client) *Resolver {
	return &Resolver{sc: sc, depotByApp: map[uint32]uint32{}}
}

// Resolve expands the given published-file ids (including collections) into a
// flat, de-duplicated list of downloadable Items for appID.
func (r *Resolver) Resolve(ctx context.Context, appID uint32, ids []uint64) ([]Item, error) {
	depot, err := r.workshopDepot(ctx, appID)
	if err != nil {
		return nil, err
	}

	var out []Item
	seen := map[uint64]bool{}
	if err := r.resolveInto(ctx, appID, depot, ids, seen, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Resolver) resolveInto(ctx context.Context, appID, depot uint32, ids []uint64, seen map[uint64]bool, out *[]Item) error {
	var pending []uint64
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			pending = append(pending, id)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	details, err := r.getDetails(ctx, pending)
	if err != nil {
		return err
	}

	for _, d := range details {
		if steamlang.EResult(d.GetResult()) != steamlang.EResult_OK {
			return fmt.Errorf("workshop: item %d lookup failed: %v", d.GetPublishedfileid(), steamlang.EResult(d.GetResult()))
		}

		if d.GetFileType() == fileTypeCollection {
			childIDs := make([]uint64, 0, len(d.GetChildren()))
			for _, c := range d.GetChildren() {
				childIDs = append(childIDs, c.GetPublishedfileid())
			}
			if err := r.resolveInto(ctx, appID, depot, childIDs, seen, out); err != nil {
				return err
			}
			continue
		}

		if d.GetHcontentFile() == 0 {
			return fmt.Errorf("workshop: item %d has no downloadable depot content (file_url-only items are not supported)", d.GetPublishedfileid())
		}

		*out = append(*out, Item{
			PubFileID:  d.GetPublishedfileid(),
			AppID:      appID,
			DepotID:    depot,
			ManifestID: d.GetHcontentFile(),
			Title:      d.GetTitle(),
		})
	}
	return nil
}

// getDetails calls PublishedFile.GetDetails for a batch of ids.
func (r *Resolver) getDetails(ctx context.Context, ids []uint64) ([]*unified.PublishedFileDetails, error) {
	req := &unified.CPublishedFile_GetDetails_Request{
		Publishedfileids: ids,
		Includechildren:  proto.Bool(true),
	}
	resp := &unified.CPublishedFile_GetDetails_Response{}
	if err := steamrpc.Call(ctx, r.sc, steamlang.EMsg_ServiceMethodCallFromClient, "PublishedFile.GetDetails#1", req, resp); err != nil {
		return nil, fmt.Errorf("workshop: get details: %w", err)
	}
	return resp.GetPublishedfiledetails(), nil
}

// workshopDepot reads the app's "workshopdepot" from PICS product info, caching it.
func (r *Resolver) workshopDepot(ctx context.Context, appID uint32) (uint32, error) {
	r.mu.Lock()
	if d, ok := r.depotByApp[appID]; ok {
		r.mu.Unlock()
		return d, nil
	}
	r.mu.Unlock()

	req := &protobuf.CMsgClientPICSProductInfoRequest{
		Apps: []*protobuf.CMsgClientPICSProductInfoRequest_AppInfo{{Appid: proto.Uint32(appID)}},
	}
	resp := &protobuf.CMsgClientPICSProductInfoResponse{}
	if err := steamrpc.Call(ctx, r.sc, steamlang.EMsg_ClientPICSProductInfoRequest, "", req, resp); err != nil {
		return 0, fmt.Errorf("workshop: product info for app %d: %w", appID, err)
	}

	for _, app := range resp.GetApps() {
		if app.GetAppid() != appID || len(app.GetBuffer()) == 0 {
			continue
		}
		// App product-info buffers are text KeyValues with a trailing null byte.
		buf := app.GetBuffer()
		if buf[len(buf)-1] == 0 {
			buf = buf[:len(buf)-1]
		}
		tree, err := vdf.ParseText(buf)
		if err != nil {
			return 0, fmt.Errorf("workshop: parse appinfo for %d: %w", appID, err)
		}
		if depot, ok := vdf.FindUint32(tree, "workshopdepot"); ok {
			r.mu.Lock()
			r.depotByApp[appID] = depot
			r.mu.Unlock()
			return depot, nil
		}
	}
	return 0, fmt.Errorf("workshop: app %d has no workshopdepot (no access, or app has no workshop content)", appID)
}
