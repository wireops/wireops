package cmd

import (
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"

	"github.com/jfxdev/wireops/internal/agent"
	"github.com/jfxdev/wireops/internal/config"
	"github.com/jfxdev/wireops/internal/docker"
	"github.com/jfxdev/wireops/internal/hooks"
	"github.com/jfxdev/wireops/internal/jobscheduler"
	"github.com/jfxdev/wireops/internal/pki"
	"github.com/jfxdev/wireops/internal/protocol"
	"github.com/jfxdev/wireops/internal/routes"
	"github.com/jfxdev/wireops/internal/sync"

	_ "github.com/jfxdev/wireops/pb_migrations"
)

// getAllowedOrigins returns the list of allowed CORS origins based on APP_URL
func getAllowedOrigins() []string {
	appURL := config.GetAppURL()
	allowedOrigins := []string{}

	// Parse APP_URL and extract origin
	if u, err := url.Parse(appURL); err == nil {
		origin := u.Scheme + "://" + u.Host
		allowedOrigins = append(allowedOrigins, origin)

		// For localhost, also allow common development ports
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" {
			allowedOrigins = append(allowedOrigins, "http://localhost:3000", "http://localhost:5173")
		}
	} else {
		log.Printf("Warning: failed to parse APP_URL '%s': %v", appURL, err)
		allowedOrigins = append(allowedOrigins, appURL)
	}

	log.Printf("[CORS] Configured allowed origins: %v", allowedOrigins)
	return allowedOrigins
}

// configureCORSMiddleware sets up CORS based on APP_URL environment variable
func configureCORSMiddleware(e *core.RequestEvent) error {
	allowedOrigins := getAllowedOrigins()
	origin := e.Request.Header.Get("Origin")

	// Check if origin is allowed
	allowed := false
	for _, o := range allowedOrigins {
		if o == origin {
			allowed = true
			break
		}
	}

	if allowed {
		e.Response.Header().Set("Access-Control-Allow-Origin", origin)
		e.Response.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		e.Response.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		e.Response.Header().Set("Access-Control-Allow-Credentials", "true")
		e.Response.Header().Set("Access-Control-Max-Age", "3600")
	}

	// Handle preflight requests
	if e.Request.Method == "OPTIONS" {
		e.Response.WriteHeader(204)
		return nil
	}

	return e.Next()
}

func Execute() error {
	_ = godotenv.Load()

	dataDir := os.Getenv("PB_DATA_DIR")
	if dataDir == "" {
		dataDir = "./pb_data"
	}

	pkiDir := os.Getenv("WIREOPS_PKI_DIR")
	if pkiDir == "" {
		pkiDir = "./pki_data"
	}
	pkiService := pki.NewService(pkiDir)
	if err := pkiService.EnsurePKI(); err != nil {
		log.Fatalf("Fatal: could not initialize PKI: %v", err)
	}

	app := pocketbase.New()
	app.RootCmd.Use = "wireops"

	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		Automigrate: true,
	})

	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		smtpHost := os.Getenv("SMTP_HOST")
		if smtpHost == "" {
			return nil
		}
		smtpPort := 587
		if portStr := os.Getenv("SMTP_PORT"); portStr != "" {
			if p, err := strconv.Atoi(portStr); err == nil {
				smtpPort = p
			}
		}
		s := app.Settings()
		s.SMTP.Enabled = true
		s.SMTP.Host = smtpHost
		s.SMTP.Port = smtpPort
		s.SMTP.Username = os.Getenv("SMTP_USERNAME")
		s.SMTP.Password = os.Getenv("SMTP_PASSWORD")
		s.SMTP.TLS = os.Getenv("SMTP_TLS") == "true"
		if sender := os.Getenv("SMTP_SENDER"); sender != "" {
			s.Meta.SenderAddress = sender
		}
		log.Printf("[smtp] configured from environment (host: %s, port: %d)", smtpHost, smtpPort)
		return nil
	})

	dockerClient, err := docker.NewClient()
	if err != nil {
		log.Printf("Warning: could not initialize Docker client: %v", err)
	}

	agentSvc := agent.NewService(app)
	mtlsServer := agent.NewMTLSServer(app, pkiService, agentSvc)
	scheduler := sync.NewScheduler(app, dockerClient, mtlsServer)
	jobSched := jobscheduler.NewScheduler(app, mtlsServer, dataDir)

	mtlsServer.SetOnConnect(func(agentID string) {
		scheduler.TriggerPendingReconciles(agentID)
		// Mark any running job_runs for this agent as failed — they were lost
		// during the disconnect and the completion message will never arrive.
		jobSched.HandleAgentReconnect(agentID)
	})

	mtlsServer.SetOnJobCompleted(func(msg protocol.JobCompletedMessage) {
		jobSched.HandleJobCompleted(msg)
	})

	go func() {
		// Use a dedicated port for agent mTLS traffic
		addr := ":8443"
		if err := mtlsServer.Start(addr); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Fatal: mTLS server failed: %v", err)
		}
	}()

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		// Sync embedded agent status and register its tags before starting the job
		// scheduler so the first cron tick always sees the agent as available.
		embeddedAgents, _ := app.FindAllRecords("agents", dbx.HashExp{"fingerprint": "embedded"})
		if len(embeddedAgents) > 0 {
			embeddedAgent := embeddedAgents[0]

			disableLocal := os.Getenv("WIREOPS_DISABLE_LOCAL_AGENT") == "true" || os.Getenv("WIREOPS_DISABLE_LOCAL_AGENT") == "1"
			targetStatus := "ACTIVE"
			if disableLocal {
				targetStatus = "REVOKED"
			}
			if embeddedAgent.GetString("status") != targetStatus {
				embeddedAgent.Set("status", targetStatus)
				if saveErr := app.Save(embeddedAgent); saveErr != nil {
					log.Printf("[AGENT] Failed to update embedded agent status: %v", saveErr)
				} else {
					log.Printf("[AGENT] Embedded agent status updated to %s", targetStatus)
				}
			}

			// Tags must be registered synchronously so the job scheduler can
			// resolve them on the very first cron tick.
			mtlsServer.SetAgentTags(embeddedAgent.Id, parseTags(os.Getenv("WIREOPS_AGENT_TAGS")))
			log.Printf("[AGENT] Embedded agent tags set: %v", parseTags(os.Getenv("WIREOPS_AGENT_TAGS")))

			// Heartbeat loop runs in the background.
			go func(agentID string) {
				intervalStr := os.Getenv("WIREOPS_HEARTBEAT_INTERVAL")
				if intervalStr == "" {
					intervalStr = "30"
				}
				intervalSecs, parseErr := strconv.Atoi(intervalStr)
				if parseErr != nil || intervalSecs <= 0 {
					intervalSecs = 30
				}

				_ = agentSvc.RecordHealthEvent(agentID, "online")
				scheduler.TriggerPendingReconciles(agentID)

				ticker := time.NewTicker(time.Duration(intervalSecs) * time.Second)
				for range ticker.C {
					_ = agentSvc.RecordHealthEvent(agentID, "online")
				}
			}(embeddedAgent.Id)
		}

		// Configure CORS middleware based on APP_URL
		se.Router.BindFunc(configureCORSMiddleware)

		se.Router.GET("/{path...}", apis.Static(os.DirFS("./pb_public"), false))

		routes.Register(se.Router, app, scheduler, dockerClient, mtlsServer)
		routes.RegisterAgentRoutes(se.Router, app, agentSvc, pkiService, mtlsServer, mtlsServer)
		routes.RegisterJobRoutes(se.Router, app, jobSched)

		if err := scheduler.Start(); err != nil {
			log.Printf("Warning: scheduler start error: %v", err)
		}
		jobSched.Start()

		// Mark job_runs stuck in "running" for more than 1 hour as "forgotten".
		app.Cron().Add("job_forgotten_sweep", "*/5 * * * *", func() {
			jobSched.MarkForgottenRuns()
		})

		// Purge job_runs older than 30 days every night at 03:00.
		app.Cron().Add("job_log_cleanup", "0 3 * * *", func() {
			result, err := app.DB().NewQuery("DELETE FROM job_runs WHERE expires_at < {:now}").
				Bind(dbx.Params{"now": time.Now()}).Execute()
			if err != nil {
				log.Printf("[cron] job_log_cleanup failed: %v", err)
				return
			}
			if n, _ := result.RowsAffected(); n > 0 {
				log.Printf("[cron] job_log_cleanup: deleted %d expired run(s)", n)
			}
		})

		return se.Next()
	})

	hooks.Register(app, scheduler, jobSched)

	// Cancel all scheduler goroutines before PocketBase tears down the DB.
	// This prevents the nil-pointer panic that occurs when a cron tick fires
	// during shutdown and calls r.app.Save() on an already-closed connection.
	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		scheduler.Shutdown()
		jobSched.Shutdown()
		return e.Next()
	})

	pbDataAbs, _ := filepath.Abs(dataDir)
	_ = os.MkdirAll(pbDataAbs, 0755)

	reposWorkspace := os.Getenv("REPOS_WORKSPACE")
	if reposWorkspace == "" {
		reposWorkspace = "./repos"
	}
	_ = os.MkdirAll(reposWorkspace, 0755)

	return app.Start()
}

// parseTags splits a comma-separated tag string into a trimmed, non-empty slice.
func parseTags(raw string) []string {
	var tags []string
	for _, t := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(t); trimmed != "" {
			tags = append(tags, trimmed)
		}
	}
	return tags
}
