package app

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wireops/wireops/pkg/logger"
	"github.com/wireops/wireops/worker/handlers"
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

func sanitizeProcessPATH() {
	safeDirs := []string{"/bin", "/sbin", "/usr/bin", "/usr/sbin", "/usr/local/bin"}
	safePath := strings.Join(safeDirs, string(filepath.ListSeparator))
	os.Setenv("PATH", safePath)
}

func cleanupLeftoverWorkdirs() {
	stackDirVar := getStackDir()
	stacksPath := filepath.Join(stackDirVar, "stacks")
	if _, err := os.Stat(stacksPath); os.IsNotExist(err) {
		return
	}

	log.Printf("[worker] using stack directory: %s (for security, ensure this path is backed by a tmpfs/in-memory filesystem)", stackDirVar)
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

func parseTags(raw string) []string {
	var tags []string
	for _, t := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(t); trimmed != "" {
			tags = append(tags, trimmed)
		}
	}
	return tags
}

func countActiveWork() int {
	return handlers.GetActiveCommandsCount() + handlers.GetActiveJobsCount()
}

func drainActiveWork(timeout, pollInterval time.Duration) bool {
	drainCtx, drainCancel := context.WithTimeout(context.Background(), timeout)
	defer drainCancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		activeCount := countActiveWork()
		if activeCount == 0 {
			return false
		}

		log.Printf("[worker] waiting for %d active tasks/jobs to finish...", activeCount)

		select {
		case <-drainCtx.Done():
			return true
		case <-ticker.C:
		}
	}
}

func Run() {
	logger.InitLogger()
	sanitizeProcessPATH()
	cleanupLeftoverWorkdirs()
	telemetry.InitWorkerInfo()

	serverURL := os.Getenv("SERVER_URL")
	workerToken := os.Getenv("WORKER_TOKEN")
	hostname := os.Getenv("HOSTNAME")

	if serverURL == "" {
		log.Fatal("SERVER_URL must be set")
	}
	if workerToken == "" {
		log.Fatal("WORKER_TOKEN must be set")
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

	tags := parseTags(os.Getenv("WORKER_TAGS"))
	backoff := initialBackoff
	stackDir := getStackDir()

	for {
		reason := transport.RunSession(serverURL, workerToken, hostname, stackDir, tags, shutdownCtx)

		switch reason {
		case transport.ReasonRevoked:
			log.Fatal("[worker] token rejected by server. Issue a new token to continue.")

		case transport.ReasonShutdown:
			log.Println("[worker] draining active tasks and jobs...")

			// 1. Close the active connection to prevent incoming commands
			transport.CloseActiveConnection()

			// 2. Wait for active tasks and jobs to drain
			shutdownTimeout := 300 * time.Second
			if envTimeout := os.Getenv("WORKER_SHUTDOWN_TIMEOUT"); envTimeout != "" {
				if val, err := strconv.Atoi(envTimeout); err == nil && val > 0 {
					shutdownTimeout = time.Duration(val) * time.Second
				}
			}

			if drainActiveWork(shutdownTimeout, 500*time.Millisecond) {
				log.Printf("[worker] shutdown timeout exceeded, forcing exit")
			} else {
				log.Println("[worker] all active commands and jobs finished gracefully")
			}

			// 3. Final flush of queues
			transport.FlushCompletedJobs()
			transport.FlushQueuedEnvelopes()

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
