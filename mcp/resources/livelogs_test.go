package resources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	wireauth "github.com/wireops/wireops/internal/auth"
	mcpauth "github.com/wireops/wireops/mcp/auth"
)

// waitFor polls cond until it's true or the timeout elapses, failing t if it
// never becomes true. Used instead of a channel handshake wherever a fixed
// request count/ordering can't be assumed (Subscribe now issues a short
// authorize probe before its long-lived watch connection, and the two race
// against each other at the fake server).
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for condition")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestSubscribeMissingAPIKey(t *testing.T) {
	b := NewLiveLogBridge("http://unused", func() *mcp.Server { return nil })
	err := b.Subscribe(context.Background(), &mcp.SubscribeRequest{
		Params: &mcp.SubscribeParams{URI: "wireops://stacks/stack1/logs/live"},
	})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestSubscribeUnsupportedURI(t *testing.T) {
	b := NewLiveLogBridge("http://unused", func() *mcp.Server { return nil })
	err := b.Subscribe(ctxWithKey(), &mcp.SubscribeRequest{
		Params: &mcp.SubscribeParams{URI: "wireops://stacks/stack1/compose"},
	})
	if err == nil {
		t.Fatal("expected error for a URI that isn't the live-logs template")
	}
}

func TestSubscribeRefCountsAndUnsubscribeStopsWatcher(t *testing.T) {
	// Server that mirrors the real /stream handler: it commits the 200
	// status line immediately (so authorize()'s synchronous probe never
	// blocks on there being log output yet), then blocks until the request
	// context is canceled, simulating a long-lived SSE connection for
	// whichever request turns out to be the actual watch. Every request —
	// probe or watch — goes through this same handler, so we track
	// arrivals/departures with counters instead of assuming an order.
	var started, finished atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		started.Add(1)
		<-r.Context().Done()
		finished.Add(1)
	}))
	defer srv.Close()

	b := NewLiveLogBridge(srv.URL, func() *mcp.Server { return nil })
	uri := "wireops://stacks/stack1/logs/live"

	if err := b.Subscribe(ctxWithKey(), &mcp.SubscribeRequest{Params: &mcp.SubscribeParams{URI: uri}}); err != nil {
		t.Fatalf("first subscribe: %v", err)
	}
	if err := b.Subscribe(ctxWithKey(), &mcp.SubscribeRequest{Params: &mcp.SubscribeParams{URI: uri}}); err != nil {
		t.Fatalf("second subscribe: %v", err)
	}

	// First subscribe issues an authorize probe (which completes and self-
	// terminates almost immediately) plus a long-lived watch connection;
	// the second subscribe reuses the existing watcher and makes no
	// network call of its own — so exactly two requests reach the server.
	waitFor(t, time.Second, func() bool { return started.Load() == 2 })
	waitFor(t, time.Second, func() bool { return finished.Load() == 1 })

	key := watcherKey{uri: uri, apiKey: "wireops_sk_test"}

	b.mu.Lock()
	w, ok := b.watchers[key]
	refCount := 0
	if ok {
		refCount = w.refCount
	}
	b.mu.Unlock()
	if !ok || refCount != 2 {
		t.Fatalf("expected one watcher with refCount=2, got ok=%v refCount=%d", ok, refCount)
	}

	// Unsubscribe reads the caller's API key from ctx the same way Subscribe
	// does, so it must carry one to resolve the same watcherKey.
	if err := b.Unsubscribe(ctxWithKey(), &mcp.UnsubscribeRequest{Params: &mcp.UnsubscribeParams{URI: uri}}); err != nil {
		t.Fatalf("first unsubscribe: %v", err)
	}
	b.mu.Lock()
	_, stillExists := b.watchers[key]
	b.mu.Unlock()
	if !stillExists {
		t.Fatal("watcher should survive while refCount > 0")
	}

	if err := b.Unsubscribe(ctxWithKey(), &mcp.UnsubscribeRequest{Params: &mcp.UnsubscribeParams{URI: uri}}); err != nil {
		t.Fatalf("second unsubscribe: %v", err)
	}
	b.mu.Lock()
	_, goneNow := b.watchers[key]
	b.mu.Unlock()
	if goneNow {
		t.Fatal("watcher should be removed once refCount reaches 0")
	}

	waitFor(t, time.Second, func() bool { return finished.Load() == 2 })
}

func TestUnsubscribeUnknownURIIsNoop(t *testing.T) {
	b := NewLiveLogBridge("http://unused", func() *mcp.Server { return nil })
	err := b.Unsubscribe(ctxWithKey(), &mcp.UnsubscribeRequest{
		Params: &mcp.UnsubscribeParams{URI: "wireops://stacks/never-subscribed/logs/live"},
	})
	if err != nil {
		t.Fatalf("expected no error unsubscribing from an unknown URI, got %v", err)
	}
}

func TestUnsubscribeMissingAPIKey(t *testing.T) {
	b := NewLiveLogBridge("http://unused", func() *mcp.Server { return nil })
	err := b.Unsubscribe(context.Background(), &mcp.UnsubscribeRequest{
		Params: &mcp.UnsubscribeParams{URI: "wireops://stacks/stack1/logs/live"},
	})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestSubscribeDifferentAPIKeyGetsSeparateWatcher(t *testing.T) {
	// Each Subscribe issues an authorize probe (short-lived) and, once that
	// succeeds, a long-lived watch connection; with two different API keys
	// both go through the network, four requests total in some order.
	var started, finished atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		started.Add(1)
		<-r.Context().Done()
		finished.Add(1)
	}))
	defer srv.Close()

	b := NewLiveLogBridge(srv.URL, func() *mcp.Server { return nil })
	uri := "wireops://stacks/stack1/logs/live"

	otherCtx := mcpauth.WithAPIKey(context.Background(), "wireops_sk_other")

	if err := b.Subscribe(ctxWithKey(), &mcp.SubscribeRequest{Params: &mcp.SubscribeParams{URI: uri}}); err != nil {
		t.Fatalf("first subscribe: %v", err)
	}
	if err := b.Subscribe(otherCtx, &mcp.SubscribeRequest{Params: &mcp.SubscribeParams{URI: uri}}); err != nil {
		t.Fatalf("second subscribe with different API key: %v", err)
	}

	waitFor(t, time.Second, func() bool { return started.Load() == 4 })
	waitFor(t, time.Second, func() bool { return finished.Load() == 2 })

	b.mu.Lock()
	watcherCount := len(b.watchers)
	first, firstOK := b.watchers[watcherKey{uri: uri, apiKey: "wireops_sk_test"}]
	second, secondOK := b.watchers[watcherKey{uri: uri, apiKey: "wireops_sk_other"}]
	b.mu.Unlock()

	// Cancel both watch goroutines' requests now, before the deferred
	// srv.Close() runs — it otherwise blocks waiting for these still-open
	// connections to finish.
	if firstOK {
		defer first.cancel()
	}
	if secondOK {
		defer second.cancel()
	}

	if watcherCount != 2 {
		t.Fatalf("expected two independently authorized watchers, got %d", watcherCount)
	}
	if !firstOK || first.refCount != 1 {
		t.Fatalf("expected refCount=1 watcher for the first API key, got %+v", first)
	}
	if !secondOK || second.refCount != 1 {
		t.Fatalf("expected refCount=1 watcher for the second API key, got %+v", second)
	}
}

// TestSubscribeRejectsUnauthorizedAPIKeyAlongsideAuthorizedWatcher covers the
// leak this bridge must close: the MCP SDK registers a session as
// subscribed to a URI purely based on Subscribe's return value, and then
// fans out every ResourceUpdated(uri) notification to *all* sessions
// subscribed to that URI — regardless of which watcher/credential produced
// it. If Subscribe returned nil for an apiKey the server would actually
// reject, that session would silently ride along on another, authorized
// session's live-log notifications for the same uri. So an unauthorized
// apiKey must fail Subscribe outright, and must never get a watcher entry,
// even while an authorized watcher for the same uri is running.
func TestSubscribeRejectsUnauthorizedAPIKeyAlongsideAuthorizedWatcher(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(wireauth.APIKeyHeader) != "wireops_sk_test" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	b := NewLiveLogBridge(srv.URL, func() *mcp.Server { return nil })
	uri := "wireops://stacks/stack1/logs/live"

	// The authorized session subscribes first and gets a real watcher.
	if err := b.Subscribe(ctxWithKey(), &mcp.SubscribeRequest{Params: &mcp.SubscribeParams{URI: uri}}); err != nil {
		t.Fatalf("authorized subscribe: %v", err)
	}
	authorizedKey := watcherKey{uri: uri, apiKey: "wireops_sk_test"}
	waitFor(t, time.Second, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		_, ok := b.watchers[authorizedKey]
		return ok
	})
	b.mu.Lock()
	if w, ok := b.watchers[authorizedKey]; ok {
		defer w.cancel()
	}
	b.mu.Unlock()

	// A different, unauthorized apiKey subscribing to the same uri must be
	// rejected outright — it must not succeed and ride along on the
	// authorized watcher's notifications, and it must not get its own
	// watcher entry either.
	unauthorizedCtx := mcpauth.WithAPIKey(context.Background(), "wireops_sk_unauthorized")
	err := b.Subscribe(unauthorizedCtx, &mcp.SubscribeRequest{Params: &mcp.SubscribeParams{URI: uri}})
	if err == nil {
		t.Fatal("expected Subscribe to fail for an apiKey the server rejects")
	}

	unauthorizedKey := watcherKey{uri: uri, apiKey: "wireops_sk_unauthorized"}
	b.mu.Lock()
	_, unauthorizedGotWatcher := b.watchers[unauthorizedKey]
	watcherCount := len(b.watchers)
	b.mu.Unlock()
	if unauthorizedGotWatcher {
		t.Fatal("unauthorized apiKey must not get a watcher entry")
	}
	if watcherCount != 1 {
		t.Fatalf("expected only the authorized watcher to remain, got %d watchers", watcherCount)
	}
}

// TestSubscribeRefusesCrossOriginRedirect covers the same header-leak class
// as mcp/client.New's CheckRedirect: Go's http.Client normally forwards
// custom request headers (including X-Wireops-Api-Key) across a redirect,
// so a malicious or misconfigured server could redirect the authorize probe
// or the SSE stream connection to another origin and harvest the caller's
// API key. NewLiveLogBridge's http.Client must reject any redirect whose
// destination isn't the configured server's origin, and the cross-origin
// target must never receive the request (header or otherwise).
func TestSubscribeRefusesCrossOriginRedirect(t *testing.T) {
	var evilGotRequest atomic.Bool
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilGotRequest.Store(true)
		if key := r.Header.Get(wireauth.APIKeyHeader); key != "" {
			t.Errorf("evil server must never receive the API key header, got %q", key)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer evil.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+r.URL.Path, http.StatusFound)
	}))
	defer origin.Close()

	b := NewLiveLogBridge(origin.URL, func() *mcp.Server { return nil })
	uri := "wireops://stacks/stack1/logs/live"

	err := b.Subscribe(ctxWithKey(), &mcp.SubscribeRequest{Params: &mcp.SubscribeParams{URI: uri}})
	if err == nil {
		t.Fatal("expected Subscribe to fail when the server redirects cross-origin")
	}

	b.mu.Lock()
	watcherCount := len(b.watchers)
	b.mu.Unlock()
	if watcherCount != 0 {
		t.Fatalf("expected no watcher to be created after a rejected cross-origin redirect, got %d", watcherCount)
	}

	if evilGotRequest.Load() {
		t.Fatal("evil (cross-origin) server should never have been contacted")
	}
}

func TestWatchRetriesAfterNon2xxAndConnectFailure(t *testing.T) {
	var mu sync.Mutex
	attempt := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempt++
		n := attempt
		mu.Unlock()

		switch n {
		case 1:
			// Non-2xx: must not be treated as a successful connection.
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		case 2:
			// Connect "succeeds" at the transport level but the handler
			// hangs up immediately with no body, simulating a drop.
			return
		default:
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			w.Write([]byte("data: recovered\n\n"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}
	}))
	defer srv.Close()

	notified := make(chan struct{})
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v0"}, &mcp.ServerOptions{
		SubscribeHandler:   func(context.Context, *mcp.SubscribeRequest) error { return nil },
		UnsubscribeHandler: func(context.Context, *mcp.UnsubscribeRequest) error { return nil },
	})

	b := NewLiveLogBridge(srv.URL, func() *mcp.Server {
		select {
		case notified <- struct{}{}:
		default:
		}
		return mcpServer
	})
	b.minBackoff = 10 * time.Millisecond
	b.maxBackoff = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go b.watch(ctx, "wireops://stacks/stack1/logs/live", "stack1", "wireops_sk_test")

	select {
	case <-notified:
	case <-time.After(5 * time.Second):
		t.Fatal("watch never recovered from the non-2xx and dropped-connection attempts to deliver a notification")
	}

	mu.Lock()
	got := attempt
	mu.Unlock()
	if got < 3 {
		t.Fatalf("expected at least 3 connection attempts (fail, drop, succeed), got %d", got)
	}
}

func TestWatchFiresResourceUpdatedOnNewLine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		w.Write([]byte("data: deploy started\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v0"}, &mcp.ServerOptions{
		SubscribeHandler:   func(context.Context, *mcp.SubscribeRequest) error { return nil },
		UnsubscribeHandler: func(context.Context, *mcp.UnsubscribeRequest) error { return nil },
	})

	var mu sync.Mutex
	notified := false
	// ResourceUpdated only fans out to subscribed sessions, none of which
	// exist in this unit test — so instead we assert indirectly: the watch
	// goroutine must reach the point of calling server.ResourceUpdated
	// without panicking or erroring on a real *mcp.Server, driven by the
	// fake SSE line above. We detect progress via the server callback below.
	b := NewLiveLogBridge(srv.URL, func() *mcp.Server {
		mu.Lock()
		notified = true
		mu.Unlock()
		return mcpServer
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go b.watch(ctx, "wireops://stacks/stack1/logs/live", "stack1", "wireops_sk_test")

	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		done := notified
		mu.Unlock()
		if done {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for watch() to process the SSE line")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
