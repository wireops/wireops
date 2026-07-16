package resources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	mcpauth "github.com/wireops/wireops/mcp/auth"
)

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
	// Server that just blocks (simulates a long-lived SSE connection) until
	// the request context is canceled, so we can assert on watcher bookkeeping
	// without depending on real stream content.
	started := make(chan struct{})
	unblocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(unblocked)
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

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the watch goroutine's request to reach the server")
	}

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

	select {
	case <-unblocked:
	case <-time.After(time.Second):
		t.Fatal("expected watch's HTTP request context to be canceled on last unsubscribe")
	}
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
	started := make(chan struct{}, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-r.Context().Done()
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

	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for both watch goroutines to reach the server")
		}
	}

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
