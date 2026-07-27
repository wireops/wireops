package app

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wireops/wireops/internal/buildinfo"
	"github.com/wireops/wireops/pkg/logger"
	"github.com/wireops/wireops/worker/handlers"
	"github.com/wireops/wireops/worker/metrics"
	"github.com/wireops/wireops/worker/telemetry"
	"github.com/wireops/wireops/worker/transport"
)

const (
	maxBackoff     = 5 * time.Minute
	initialBackoff = 5 * time.Second
)

var defaultStackDir string

func init() {
	home, err := os.UserHomeDir()
	if err == nil {
		defaultStackDir = filepath.Join(home, ".wireops")
		return
	}
	tempDir, err := os.MkdirTemp("", "wireops-*")
	if err == nil {
		defaultStackDir = tempDir
		return
	}
	if cwd, err := os.Getwd(); err == nil {
		defaultStackDir = filepath.Join(cwd, ".wireops")
		return
	}
	defaultStackDir = "./.wireops"
}

func getSecureDefaultStackDir() string {
	return defaultStackDir
}

func getStackDir() string {
	stackDirVar := strings.TrimSpace(os.Getenv("WORKER_STACK_DIR"))
	if stackDirVar == "" {
		return getSecureDefaultStackDir()
	}
	return stackDirVar
}

func getHealthAddr() string {
	addr := strings.TrimSpace(os.Getenv("WORKER_HEALTH_ADDR"))
	if addr == "" {
		return ":8095"
	}
	return addr
}

// startHealthServer runs a local-only HTTP endpoint reporting whether the
// worker currently holds a live WebSocket session with the server, so
// Docker/orchestrator healthchecks can tell "process alive" apart from
// "actually doing its job".
func startHealthServer(ctx context.Context, addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt64(&metrics.Connected) == 1 {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("disconnected"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("[worker] health endpoint bind error addr=%s error=%v (healthcheck will be unavailable)", addr, err)
		return
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	go func() {
		log.Printf("[worker] health endpoint listening addr=%s", addr)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[worker] health endpoint error: %v", err)
		}
	}()
}

func sanitizeProcessPATH() {
	safeDirs := []string{"/bin", "/sbin", "/usr/bin", "/usr/sbin", "/usr/local/bin"}
	safePath := strings.Join(safeDirs, string(filepath.ListSeparator))
	os.Setenv("PATH", safePath)
}

func cleanupLeftoverWorkdirs(stackDirVar string) {
	stacksPath := filepath.Join(stackDirVar, "stacks")
	if _, err := os.Stat(stacksPath); os.IsNotExist(err) {
		return
	}

	log.Printf("[worker] checking for leftover work directories in %s...", stacksPath)

	stackDirs, err := os.ReadDir(stacksPath)
	if err != nil {
		return
	}
	for _, sd := range stackDirs {
		if !sd.IsDir() {
			continue
		}
		sdPath := filepath.Join(stacksPath, sd.Name())
		cmdDirs, err := os.ReadDir(sdPath)
		if err != nil {
			continue
		}
		for _, cd := range cmdDirs {
			if !cd.IsDir() || !strings.HasPrefix(cd.Name(), "cmd-") {
				continue
			}
			pathToDelete := filepath.Join(sdPath, cd.Name())
			log.Printf("[worker] cleaning up leftover workdir: %s", pathToDelete)
			_ = os.RemoveAll(pathToDelete)
		}
	}
}

var validTagPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func parseTags(raw string) ([]string, error) {
	var tags []string
	for _, t := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(t)
		if trimmed == "" {
			continue
		}
		if !validTagPattern.MatchString(trimmed) {
			return nil, fmt.Errorf("invalid WORKER_TAGS entry %q: tags must be comma-separated and contain only letters, numbers, - or _ (no spaces or special characters)", trimmed)
		}
		tags = append(tags, trimmed)
	}
	return tags, nil
}

func Run() {
	logger.InitLogger()
	log.Printf("[worker] starting version=%s commit=%s build_date=%s", buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate)
	sanitizeProcessPATH()
	stackDir := getStackDir()
	log.Printf("[worker] using stack directory: %s (for security, ensure this path is backed by a tmpfs/in-memory filesystem)", stackDir)
	cleanupLeftoverWorkdirs(stackDir)
	telemetry.InitWorkerInfo()

	serverURL := os.Getenv("SERVER_URL")
	workerToken := os.Getenv("WORKER_TOKEN")
	hostname := os.Getenv("HOSTNAME")
	workerTags := os.Getenv("WORKER_TAGS")

	if serverURL == "" {
		log.Fatal("SERVER_URL must be set")
	}
	if workerToken == "" {
		log.Fatal("WORKER_TOKEN must be set")
	}
	tags, err := parseTags(workerTags)
	if err != nil {
		log.Fatalf("[worker] %v", err)
	}
	if len(tags) == 0 {
		log.Fatal("WORKER_TAGS must be set")
	}
	if hostname == "" {
		h, err := os.Hostname()
		if err == nil {
			hostname = h
		} else {
			hostname = "unknown-worker"
		}
	}

	heavyLimit := 3
	if limitStr := os.Getenv("WORKER_HEAVY_CONCURRENCY"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
			heavyLimit = val
		}
	} else if limitStr := os.Getenv("WORKER_MAX_CONCURRENT_TASKS"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
			heavyLimit = val
		}
	}

	lightLimit := 5
	if limitStr := os.Getenv("WORKER_LIGHT_CONCURRENCY"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
			lightLimit = val
		}
	}

	interactiveLimit := 3
	if limitStr := os.Getenv("WORKER_INTERACTIVE_CONCURRENCY"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
			interactiveLimit = val
		}
	}

	maxQueueDepth := 50
	if depthStr := os.Getenv("WORKER_MAX_QUEUE_DEPTH"); depthStr != "" {
		if val, err := strconv.Atoi(depthStr); err == nil && val > 0 {
			maxQueueDepth = val
		}
	}

	handlers.InitSemaphores(heavyLimit, lightLimit, interactiveLimit, maxQueueDepth)

	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("[worker] received signal %v, starting graceful shutdown...", sig)
		shutdownCancel()
	}()

	startHealthServer(shutdownCtx, getHealthAddr())

	backoff := initialBackoff
	for {
		reason, connected := transport.RunSession(serverURL, workerToken, hostname, stackDir, tags, shutdownCtx)

		if connected {
			backoff = initialBackoff
		}

		switch reason {
		case transport.ReasonRevoked:
			if err := transport.PurgeSpool(); err != nil {
				log.Printf("[worker] failed to purge spool after revocation: %v", err)
			}
			log.Fatal("[worker] token rejected by server. Issue a new token to continue.")

		case transport.ReasonShutdown:
			log.Println("[worker] shutdown complete")
			return

		default:
			log.Printf("[worker] disconnected reconnecting_in=%v", backoff)
			select {
			case <-shutdownCtx.Done():
				// shutdown requested, proceed to next iteration to handle ReasonShutdown
			case <-time.After(backoff):
				backoff = min(backoff*2, maxBackoff)
			}
		}
	}
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
