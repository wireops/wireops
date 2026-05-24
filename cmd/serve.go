package cmd

import (
	"fmt"
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

	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/docker"
	"github.com/wireops/wireops/internal/hooks"
	"github.com/wireops/wireops/internal/jobscheduler"
	"github.com/wireops/wireops/internal/oidc"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/routes"
	"github.com/wireops/wireops/internal/sync"
	"github.com/wireops/wireops/internal/worker"

	_ "github.com/wireops/wireops/internal/integrations/dozzle"
	_ "github.com/wireops/wireops/internal/integrations/traefik"

	_ "github.com/wireops/wireops/pb_migrations"
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
		if smtpHost != "" {
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
		}

		configureOIDC(app)

		return nil
	})

	dockerClient, err := docker.NewClient()
	if err != nil {
		log.Printf("Warning: could not initialize Docker client: %v", err)
	}

	workerSvc := worker.NewService(app)
	workerServer := worker.NewWorkerServer(app, workerSvc)
	scheduler := sync.NewScheduler(app, dockerClient, workerServer)
	jobSched := jobscheduler.NewScheduler(app, workerServer, dataDir)

	workerServer.SetOnConnect(func(workerID string) {
		scheduler.TriggerPendingReconciles(workerID)
		// Mark any running job_runs for this worker as failed — they were lost
		// during the disconnect and the completion message will never arrive.
		jobSched.HandleWorkerReconnect(workerID)
	})

	workerServer.SetOnJobCompleted(func(msg protocol.JobCompletedMessage) {
		jobSched.HandleJobCompleted(msg)
	})

	go func() {
		addr := ":8443"
		if err := workerServer.Start(addr); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Fatal: worker server failed: %v", err)
		}
	}()

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		// OIDC client secret lives only in OIDC_CLIENT_SECRET; inject into the collection cache
		// for OAuth handlers, then reload the cache so it is not retained for other routes.
		se.Router.BindFunc(hooks.SSOUsersOAuthRuntimeMiddleware(app))

		// Configure CORS middleware based on APP_URL
		se.Router.BindFunc(configureCORSMiddleware)

		se.Router.GET("/{path...}", apis.Static(os.DirFS("./pb_public"), false))

		routes.RegisterSetupRoutes(se.Router, app)
		routes.Register(se.Router, app, scheduler, dockerClient, workerServer)
		routes.RegisterWorkerRoutes(se.Router, app, workerSvc, workerServer, workerServer)
		routes.RegisterJobRoutes(se.Router, app, jobSched)
		routes.RegisterAuthRoutes(se.Router, app)

		if err := scheduler.Start(); err != nil {
			log.Printf("Warning: scheduler start error: %v", err)
		}
		jobSched.Start()

		// Mark job_runs stuck in "running" for more than 1 hour as "forgotten".
		app.Cron().Add("job_forgotten_sweep", "*/5 * * * *", func() {
			jobSched.MarkForgottenRuns()
		})

		app.Cron().Add("worker_token_expiry", "*/5 * * * *", func() {
			if err := workerSvc.ExpireStagingTokens(); err != nil {
				log.Printf("[WORKER] Failed to expire staging tokens: %v", err)
			}
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

// validateOIDCURL validates that a URL is well-formed and uses HTTPS in production.
// Returns an error if the URL is invalid or insecure.
// OIDC_USER_INFO_URL may be empty (user data then comes from id_token claims).
func validateOIDCURL(urlStr, fieldName string) error {
	if urlStr == "" {
		if fieldName == "OIDC_USER_INFO_URL" {
			return nil
		}
		return fmt.Errorf("%s is required", fieldName)
	}

	parsed, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("%s is not a valid URL: %v", fieldName, err)
	}

	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("%s must use http or https scheme", fieldName)
	}

	if parsed.Host == "" {
		return fmt.Errorf("%s must have a host", fieldName)
	}

	// In production (when APP_URL uses https), require HTTPS for OIDC URLs
	appURL := config.GetAppURL()
	if strings.HasPrefix(appURL, "https://") && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use HTTPS in production", fieldName)
	}

	return nil
}

// persistSSOUsersOAuth2Disabled clears OAuth2 on sso_users and saves so invalid env does not leave a stale provider.
func persistSSOUsersOAuth2Disabled(app core.App, col *core.Collection, logCtx string) {
	col.OAuth2.Enabled = false
	col.OAuth2.Providers = nil
	if err := app.Save(col); err != nil {
		log.Printf("[oidc] failed to persist disabled OAuth2 on sso_users (%s): %v", logCtx, err)
	}
}

// configureOIDC reads OIDC env vars and updates the sso_users collection accordingly.
// If OIDC_CLIENT_ID is set, it enables the oidc OAuth2 provider on sso_users.
// If it is empty, it disables OAuth2 so listAuthMethods returns no providers.
// The client secret is not saved: set OIDC_CLIENT_SECRET in the environment (injected at OAuth request time).
func configureOIDC(app core.App) {
	col, err := app.FindCollectionByNameOrId("sso_users")
	if err != nil {
		log.Printf("[oidc] sso_users collection not found, skipping OIDC setup: %v", err)
		return
	}

	clientID := os.Getenv("OIDC_CLIENT_ID")
	if clientID == "" {
		if col.OAuth2.Enabled || len(col.OAuth2.Providers) > 0 {
			persistSSOUsersOAuth2Disabled(app, col, "OIDC_CLIENT_ID unset")
		}
		return
	}

	// Validate OIDC URLs before configuring
	authURL := os.Getenv("OIDC_AUTH_URL")
	tokenURL := os.Getenv("OIDC_TOKEN_URL")
	userInfoURL := os.Getenv("OIDC_USER_INFO_URL")

	if err := validateOIDCURL(authURL, "OIDC_AUTH_URL"); err != nil {
		log.Printf("[oidc] configuration error: %v", err)
		persistSSOUsersOAuth2Disabled(app, col, "invalid OIDC_AUTH_URL")
		return
	}
	if err := validateOIDCURL(tokenURL, "OIDC_TOKEN_URL"); err != nil {
		log.Printf("[oidc] configuration error: %v", err)
		persistSSOUsersOAuth2Disabled(app, col, "invalid OIDC_TOKEN_URL")
		return
	}
	if err := validateOIDCURL(userInfoURL, "OIDC_USER_INFO_URL"); err != nil {
		log.Printf("[oidc] configuration error: %v", err)
		persistSSOUsersOAuth2Disabled(app, col, "invalid OIDC_USER_INFO_URL")
		return
	}

	displayName := os.Getenv("OIDC_DISPLAY_NAME")
	if displayName == "" {
		displayName = "SSO"
	}

	// Client secret is never stored; use OIDC_CLIENT_SECRET at runtime (see SSOUsersOAuthRuntimeMiddleware).
	col.OAuth2.Enabled = true
	col.OAuth2.Providers = []core.OAuth2ProviderConfig{{
		Name:         "oidc",
		ClientId:     clientID,
		ClientSecret: "",
		AuthURL:      authURL,
		TokenURL:     tokenURL,
		UserInfoURL:  userInfoURL,
		DisplayName:  displayName,
	}}
	oidc.HydrateClientSecretForValidation(col)

	if err := app.Save(col); err != nil {
		log.Printf("[oidc] failed to configure OIDC provider on sso_users: %v", err)
		return
	}
	log.Printf("[oidc] configured provider '%s' on sso_users", displayName)
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
