package resources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

	b.mu.Lock()
	w, ok := b.watchers[uri]
	refCount := 0
	if ok {
		refCount = w.refCount
	}
	b.mu.Unlock()
	if !ok || refCount != 2 {
		t.Fatalf("expected one watcher with refCount=2, got ok=%v refCount=%d", ok, refCount)
	}

	if err := b.Unsubscribe(context.Background(), &mcp.UnsubscribeRequest{Params: &mcp.UnsubscribeParams{URI: uri}}); err != nil {
		t.Fatalf("first unsubscribe: %v", err)
	}
	b.mu.Lock()
	_, stillExists := b.watchers[uri]
	b.mu.Unlock()
	if !stillExists {
		t.Fatal("watcher should survive while refCount > 0")
	}

	if err := b.Unsubscribe(context.Background(), &mcp.UnsubscribeRequest{Params: &mcp.UnsubscribeParams{URI: uri}}); err != nil {
		t.Fatalf("second unsubscribe: %v", err)
	}
	b.mu.Lock()
	_, goneNow := b.watchers[uri]
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
	err := b.Unsubscribe(context.Background(), &mcp.UnsubscribeRequest{
		Params: &mcp.UnsubscribeParams{URI: "wireops://stacks/never-subscribed/logs/live"},
	})
	if err != nil {
		t.Fatalf("expected no error unsubscribing from an unknown URI, got %v", err)
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
