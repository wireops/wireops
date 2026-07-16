package resources

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

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

	// minBackoff/maxBackoff bound the retry delay in watch; overridable by
	// tests, defaulted by NewLiveLogBridge.
	minBackoff, maxBackoff time.Duration

	mu       sync.Mutex
	watchers map[watcherKey]*liveLogWatcher
}

// watcherKey scopes a watcher to both the resource URI and the API key that
// started it, so refCount reuse never hands a caller notifications sourced
// from a connection opened with a different, unrelated credential.
type watcherKey struct {
	uri    string
	apiKey string
}

type liveLogWatcher struct {
	cancel   context.CancelFunc
	apiKey   string
	refCount int
}

// NewLiveLogBridge creates a bridge that opens SSE connections against
// serverURL and notifies through server(). server is a func rather than a
// *mcp.Server because the bridge must be wired into ServerOptions before
// mcp.NewServer returns the server it needs to call back into.
func NewLiveLogBridge(serverURL string, server func() *mcp.Server) *LiveLogBridge {
	trimmed := strings.TrimRight(serverURL, "/")
	origin, _ := url.Parse(trimmed)

	return &LiveLogBridge{
		serverURL: trimmed,
		server:    server,
		httpc: &http.Client{
			// Redirects normally carry forward custom request headers, so a
			// redirect to another origin would leak X-Wireops-Api-Key to it
			// (both the authorize probe and the SSE stream connection send
			// this header). Only permit redirects that stay on serverURL's
			// origin — mirrors mcp/client.New's CheckRedirect.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if origin == nil || req.URL.Scheme != origin.Scheme || req.URL.Host != origin.Host {
					return fmt.Errorf("refusing cross-origin redirect to %s", req.URL)
				}
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				return nil
			},
		},
		minBackoff: 500 * time.Millisecond,
		maxBackoff: 30 * time.Second,
		watchers:   make(map[watcherKey]*liveLogWatcher),
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

	key := watcherKey{uri: uri, apiKey: apiKey}

	// Reuse is keyed by (uri, apiKey), so this only ever matches a watcher
	// started with the same credential — a different apiKey for the same
	// uri always falls through and gets its own independently authorized
	// connection below.
	b.mu.Lock()
	if w, exists := b.watchers[key]; exists {
		w.refCount++
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	// Authorize synchronously, outside the lock, before Subscribe can
	// return success. The MCP SDK registers this session as subscribed to
	// uri as soon as Subscribe returns nil, and ResourceUpdated fans out
	// to every session subscribed to that uri — not just the one whose
	// watcher produced the line. Returning nil for an apiKey that can't
	// actually read this stack's logs would let it ride along on another,
	// authorized session's watcher once one exists.
	if err := b.authorize(ctx, stackID, apiKey); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if w, exists := b.watchers[key]; exists {
		w.refCount++
		return nil
	}

	watchCtx, cancel := context.WithCancel(context.Background())
	b.watchers[key] = &liveLogWatcher{cancel: cancel, apiKey: apiKey, refCount: 1}
	go b.watch(watchCtx, uri, stackID, apiKey)
	return nil
}

// authorize makes a synchronous request to confirm apiKey can read
// stackID's log stream before Subscribe is allowed to succeed for it.
func (b *LiveLogBridge) authorize(ctx context.Context, stackID, apiKey string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.serverURL+"/api/custom/stacks/"+stackID+"/stream", nil)
	if err != nil {
		return err
	}
	req.Header.Set(wireauth.APIKeyHeader, apiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := b.httpc.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("not authorized to subscribe to stack %s logs: server returned status %d", stackID, resp.StatusCode)
	}
	return nil
}

// Unsubscribe implements mcp.ServerOptions.UnsubscribeHandler.
func (b *LiveLogBridge) Unsubscribe(ctx context.Context, req *mcp.UnsubscribeRequest) error {
	uri := req.Params.URI
	apiKey, err := apiKeyFrom(ctx)
	if err != nil {
		return err
	}
	key := watcherKey{uri: uri, apiKey: apiKey}

	b.mu.Lock()
	defer b.mu.Unlock()

	w, exists := b.watchers[key]
	if !exists {
		return nil
	}
	w.refCount--
	if w.refCount <= 0 {
		w.cancel()
		delete(b.watchers, key)
	}
	return nil
}

// watch repeatedly streams GET /api/custom/stacks/{id}/stream and fires a
// resources/updated notification for every new SSE "data:" line. A dropped
// connection, a connect error, or a non-2xx response is retried with bounded
// backoff rather than ending the goroutine — watch only returns when ctx is
// canceled (the last Unsubscribe for this uri+apiKey), which is exactly when
// the caller removes the watcher from b.watchers, so the map never holds an
// entry whose goroutine has already exited.
func (b *LiveLogBridge) watch(ctx context.Context, uri, stackID, apiKey string) {
	backoff := b.minBackoff

	for ctx.Err() == nil {
		connected := b.watchOnce(ctx, uri, stackID, apiKey)
		if ctx.Err() != nil {
			return
		}
		if connected {
			backoff = b.minBackoff
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > b.maxBackoff {
			backoff = b.maxBackoff
		}
	}
}

// watchOnce makes a single SSE connection attempt and streams it until it
// ends or ctx is canceled. It reports whether the connection was actually
// established (2xx response), so watch can reset its backoff even if the
// stream drops shortly after.
func (b *LiveLogBridge) watchOnce(ctx context.Context, uri, stackID, apiKey string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.serverURL+"/api/custom/stacks/"+stackID+"/stream", nil)
	if err != nil {
		return false
	}
	req.Header.Set(wireauth.APIKeyHeader, apiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := b.httpc.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}

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
	return true
}
