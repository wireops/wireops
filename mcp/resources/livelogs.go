package resources

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	wireauth "github.com/wireops/wireops/internal/auth"
)

// LiveLogBridge implements the MCP resources/subscribe and
// resources/unsubscribe handlers for wireops://stacks/{id}/logs/live. It
// keeps at most one background SSE connection to the wireops server's
// (now genuinely live, see internal/logstream) GET
// /api/custom/stacks/{id}/stream endpoint per subscribed URI, ref-counted
// across MCP sessions, and turns each new line into a resources/updated
// notification — per MCP semantics that's a "go re-read the resource"
// signal, not a raw content push (see mcp.Server.ResourceUpdated).
type LiveLogBridge struct {
	serverURL string
	server    func() *mcp.Server
	httpc     *http.Client

	mu       sync.Mutex
	watchers map[string]*liveLogWatcher
}

type liveLogWatcher struct {
	cancel   context.CancelFunc
	refCount int
}

// NewLiveLogBridge creates a bridge that opens SSE connections against
// serverURL and notifies through server(). server is a func rather than a
// *mcp.Server because the bridge must be wired into ServerOptions before
// mcp.NewServer returns the server it needs to call back into.
func NewLiveLogBridge(serverURL string, server func() *mcp.Server) *LiveLogBridge {
	return &LiveLogBridge{
		serverURL: strings.TrimRight(serverURL, "/"),
		server:    server,
		httpc:     &http.Client{},
		watchers:  make(map[string]*liveLogWatcher),
	}
}

// Subscribe implements mcp.ServerOptions.SubscribeHandler.
func (b *LiveLogBridge) Subscribe(ctx context.Context, req *mcp.SubscribeRequest) error {
	uri := req.Params.URI
	prefix, suffix, _ := strings.Cut(stackLiveLogsURITemplate, "{id}")
	stackID, ok := extractID(uri, prefix, suffix)
	if !ok {
		return fmt.Errorf("unsupported resource URI for subscription: %s", uri)
	}
	apiKey, err := apiKeyFrom(ctx)
	if err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if w, exists := b.watchers[uri]; exists {
		w.refCount++
		return nil
	}

	watchCtx, cancel := context.WithCancel(context.Background())
	b.watchers[uri] = &liveLogWatcher{cancel: cancel, refCount: 1}
	go b.watch(watchCtx, uri, stackID, apiKey)
	return nil
}

// Unsubscribe implements mcp.ServerOptions.UnsubscribeHandler.
func (b *LiveLogBridge) Unsubscribe(_ context.Context, req *mcp.UnsubscribeRequest) error {
	uri := req.Params.URI

	b.mu.Lock()
	defer b.mu.Unlock()

	w, exists := b.watchers[uri]
	if !exists {
		return nil
	}
	w.refCount--
	if w.refCount <= 0 {
		w.cancel()
		delete(b.watchers, uri)
	}
	return nil
}

// watch streams GET /api/custom/stacks/{id}/stream and fires a
// resources/updated notification for every new SSE "data:" line, until
// ctx is canceled (last unsubscribe) or the connection drops.
func (b *LiveLogBridge) watch(ctx context.Context, uri, stackID, apiKey string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.serverURL+"/api/custom/stacks/"+stackID+"/stream", nil)
	if err != nil {
		return
	}
	req.Header.Set(wireauth.APIKeyHeader, apiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := b.httpc.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		server := b.server()
		if server == nil {
			continue
		}
		_ = server.ResourceUpdated(ctx, &mcp.ResourceUpdatedNotificationParams{URI: uri})
	}
}
