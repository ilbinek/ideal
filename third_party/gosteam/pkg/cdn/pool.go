package cdn

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	steam "github.com/0xAozora/go-steam"
	"github.com/0xAozora/go-steam/protocol/protobuf/unified"
	"github.com/0xAozora/go-steam/protocol/steamlang"
	"google.golang.org/protobuf/proto"

	"github.com/tbdevent/gosteam/pkg/steamrpc"
)

// Server is a SteamPipe content server we can fetch manifests and chunks from.
type Server struct {
	Host         string
	Scheme       string // "https" or "http"
	WeightedLoad float32

	penalty int // incremented on failures; used to deprioritize bad servers
}

// baseURL returns the scheme://host prefix for building request URLs.
func (s *Server) baseURL() string { return s.Scheme + "://" + s.Host }

// Pool holds the content server list for a cell and hands out servers in
// weighted, round-robin order, deprioritizing servers that have failed. It also
// caches per-host CDN auth tokens. Safe for concurrent use.
type Pool struct {
	sc     *steam.Client
	cellID uint32

	mu      sync.Mutex
	servers []*Server
	next    int

	tokens map[string]cdnToken // host -> token
}

type cdnToken struct {
	value   string
	expires time.Time
}

// NewPool fetches the content server list for the session's cell and returns a
// ready-to-use pool.
func NewPool(ctx context.Context, sc *steam.Client, cellID uint32) (*Pool, error) {
	p := &Pool{sc: sc, cellID: cellID, tokens: map[string]cdnToken{}}
	if err := p.refresh(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// refresh (re)loads the server list via ContentServerDirectory.GetServersForSteamPipe.
func (p *Pool) refresh(ctx context.Context) error {
	maxServers := uint32(20)
	req := &unified.CContentServerDirectory_GetServersForSteamPipe_Request{
		CellId:     proto.Uint32(p.cellID),
		MaxServers: &maxServers,
	}
	resp := &unified.CContentServerDirectory_GetServersForSteamPipe_Response{}
	if err := steamrpc.Call(ctx, p.sc, steamlang.EMsg_ServiceMethodCallFromClient, "ContentServerDirectory.GetServersForSteamPipe#1", req, resp); err != nil {
		return fmt.Errorf("cdn: get servers: %w", err)
	}

	var servers []*Server
	for _, s := range resp.GetServers() {
		// Use only direct content servers; proxy/Lancache servers need path
		// templating that we don't implement yet.
		if s.GetUseAsProxy() {
			continue
		}
		typ := s.GetType()
		if typ != "SteamCache" && typ != "CDN" {
			continue
		}
		scheme := "http"
		if s.GetHttpsSupport() == "mandatory" || s.GetHttpsSupport() == "optional" {
			scheme = "https"
		}
		host := s.GetVhost()
		if host == "" {
			host = s.GetHost()
		}
		if host == "" {
			continue
		}
		servers = append(servers, &Server{Host: host, Scheme: scheme, WeightedLoad: s.GetWeightedLoad()})
	}
	if len(servers) == 0 {
		return fmt.Errorf("cdn: no usable content servers returned for cell %d", p.cellID)
	}
	// Prefer lower weighted load.
	sort.SliceStable(servers, func(i, j int) bool { return servers[i].WeightedLoad < servers[j].WeightedLoad })

	p.mu.Lock()
	p.servers = servers
	p.next = 0
	p.mu.Unlock()
	return nil
}

// Get returns the next server to try, preferring servers with fewer failures.
func (p *Pool) Get() *Server {
	p.mu.Lock()
	defer p.mu.Unlock()

	best := p.servers[p.next%len(p.servers)]
	// Scan for a server with the lowest penalty, starting at the round-robin cursor.
	for i := 0; i < len(p.servers); i++ {
		s := p.servers[(p.next+i)%len(p.servers)]
		if s.penalty < best.penalty {
			best = s
		}
	}
	p.next++
	return best
}

// Penalize records a failure against a server so Get deprioritizes it.
func (p *Pool) Penalize(s *Server) {
	p.mu.Lock()
	s.penalty++
	p.mu.Unlock()
}

// Token returns a (possibly empty) CDN auth token for host, fetching and caching
// it via ContentServerDirectory.GetCDNAuthToken when needed. Many *.steamcontent.com
// hosts return an empty token, which is fine.
func (p *Pool) Token(ctx context.Context, appID, depotID uint32, host string) (string, error) {
	p.mu.Lock()
	t, ok := p.tokens[host]
	p.mu.Unlock()
	if ok && time.Now().Before(t.expires) {
		return t.value, nil
	}

	req := &unified.CContentServerDirectory_GetCDNAuthToken_Request{
		AppId:    proto.Uint32(appID),
		DepotId:  proto.Uint32(depotID),
		HostName: proto.String(host),
	}
	resp := &unified.CContentServerDirectory_GetCDNAuthToken_Response{}
	if err := steamrpc.Call(ctx, p.sc, steamlang.EMsg_ServiceMethodCallFromClient, "ContentServerDirectory.GetCDNAuthToken#1", req, resp); err != nil {
		return "", fmt.Errorf("cdn: get auth token: %w", err)
	}

	tok := cdnToken{value: resp.GetToken()}
	if exp := resp.GetExpirationTime(); exp > 0 {
		tok.expires = time.Unix(int64(exp), 0)
	} else {
		tok.expires = time.Now().Add(10 * time.Minute)
	}
	p.mu.Lock()
	p.tokens[host] = tok
	p.mu.Unlock()
	return tok.value, nil
}
