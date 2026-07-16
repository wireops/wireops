// Package app wires up the wireops MCP server: a standalone deployable,
// analogous to worker/app, that exposes read-only wireops data as MCP
// tools over streamable HTTP. It never authenticates to the wireops
// server with a credential of its own — every caller supplies their own
// viewer-scoped API key on each connection (pass-through auth).
package app

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wireops/wireops/internal/auth"
	"github.com/wireops/wireops/internal/buildinfo"
	mcpauth "github.com/wireops/wireops/mcp/auth"
	"github.com/wireops/wireops/mcp/client"
	"github.com/wireops/wireops/mcp/prompts"
	"github.com/wireops/wireops/mcp/resources"
	"github.com/wireops/wireops/mcp/tools"
)

func getListenAddr() string {
	addr := strings.TrimSpace(os.Getenv("MCP_LISTEN_ADDR"))
	if addr == "" {
		return ":8091"
	}
	return addr
}

// withAPIKeyMiddleware extracts the caller's wireops API key from the
// inbound MCP request and stashes it in the request context so tool
// handlers can forward it. The MCP process itself never persists it.
func withAPIKeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get(auth.APIKeyHeader)
		if apiKey == "" {
			if bearer := r.Header.Get("Authorization"); strings.HasPrefix(bearer, "Bearer ") {
				apiKey = strings.TrimPrefix(bearer, "Bearer ")
			}
		}
		if apiKey != "" {
			r = r.WithContext(mcpauth.WithAPIKey(r.Context(), apiKey))
		}
		next.ServeHTTP(w, r)
	})
}

// Run starts the MCP HTTP server and blocks until it receives SIGINT/SIGTERM.
func Run() {
	log.Printf("[mcp] starting version=%s commit=%s build_date=%s", buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate)

	serverURL := strings.TrimSpace(os.Getenv("SERVER_URL"))
	if serverURL == "" {
		log.Fatal("SERVER_URL must be set")
	}

	wireopsClient := client.New(serverURL)

	// mcpServer is captured by the live-log bridge's SubscribeHandler/
	// UnsubscribeHandler closures before it exists (ServerOptions must be
	// passed into the constructor that returns it) — the closures only call
	// back into it once requests start arriving, by which point it's set.
	var mcpServer *sdkmcp.Server
	liveLogBridge := resources.NewLiveLogBridge(serverURL, func() *sdkmcp.Server { return mcpServer })

	mcpServer = sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "wireops-mcp",
		Version: buildinfo.Version,
	}, &sdkmcp.ServerOptions{
		SubscribeHandler:   liveLogBridge.Subscribe,
		UnsubscribeHandler: liveLogBridge.Unsubscribe,
	})
	tools.Register(mcpServer, wireopsClient)
	resources.Register(mcpServer, wireopsClient)
	prompts.Register(mcpServer, wireopsClient)

	streamableHandler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return mcpServer
	}, nil)

	mux := http.NewServeMux()
	mux.Handle("/mcp", withAPIKeyMiddleware(streamableHandler))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	listenAddr := getListenAddr()
	httpServer := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("[mcp] received signal %v, shutting down...", sig)
		shutdownCancel()
	}()

	go func() {
		<-shutdownCtx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Printf("[mcp] graceful shutdown error: %v", err)
		}
	}()

	log.Printf("[mcp] listening addr=%s wireops_server=%s", listenAddr, serverURL)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[mcp] server error: %v", err)
	}
	log.Println("[mcp] shutdown complete")
}
