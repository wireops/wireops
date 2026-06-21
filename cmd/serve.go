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
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"

	"github.com/wireops/wireops/internal/audit"
	wireauth "github.com/wireops/wireops/internal/auth"
	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/docker"
	"github.com/wireops/wireops/internal/hooks"
	"github.com/wireops/wireops/internal/jobscheduler"
	"github.com/wireops/wireops/internal/oidc"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/routes"
	wiresync "github.com/wireops/wireops/internal/sync"
	"github.com/wireops/wireops/internal/worker"
	"github.com/wireops/wireops/pkg/logger"
	wiretls "github.com/wireops/wireops/pkg/tls"

	_ "github.com/wireops/wireops/internal/integrations/dozzle"
	_ "github.com/wireops/wireops/internal/integrations/ntfy"
	_ "github.com/wireops/wireops/internal/integrations/traefik"
	_ "github.com/wireops/wireops/internal/integrations/webhook"

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
	// Load .env before InitLogger so LOG_LEVEL and other vars are visible
	// when the logger initialises. The error is intentionally ignored: .env is
	// optional in production (env vars may be injected by the runtime instead).
	err := godotenv.Load()
	logger.InitLogger()
	if err == nil {
		log.Println("[config] loaded environment from .env")
	}

	dataDir := os.Getenv("PB_DATA_DIR")
	if dataDir == "" {
		dataDir = "./pb_data"
	}

	app := pocketbase.NewWithConfig(pocketbase.Config{
		HideStartBanner: true,
	})
	app.RootCmd.Use = "wireops"

	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		Automigrate: true,
	})

	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		isMigrationCmd := false
		if len(os.Args) < 2 {
			isMigrationCmd = true // default command is serve
		} else {
			cmd := os.Args[1]
			if cmd == "serve" || cmd == "migrate" {
				isMigrationCmd = true
			}
		}

		if isMigrationCmd {
			log.Println("[db] Running database migrations...")
		}

		if err := e.Next(); err != nil {
			audit.RecordSystem(app, "schema_migration", "database", "pocketbase", audit.StatusError, "migration_failed")
			if isMigrationCmd {
				log.Printf("[db] Database migrations failed: %v", err)
			}
			return err
		}
		audit.RecordSystem(app, "schema_migration", "database", "pocketbase", audit.StatusSuccess, "")

		// Disable all PocketBase dbx SQL logging. All three hooks (LogFunc,
		// QueryLogFunc, ExecLogFunc) pass the full SQL body including parameter
		// values to the logger, which would leak sensitive data from the output
		// and other fields stored in job_runs and sync_logs.
		redirectDB := func(dbConn *dbx.DB) {
			dbConn.LogFunc = nil
			dbConn.QueryLogFunc = nil
			dbConn.ExecLogFunc = nil
		}

		if dbConn, ok := app.DB().(*dbx.DB); ok {
			redirectDB(dbConn)
		}
		if dbConn, ok := app.ConcurrentDB().(*dbx.DB); ok {
			redirectDB(dbConn)
		}
		if dbConn, ok := app.NonconcurrentDB().(*dbx.DB); ok {
			redirectDB(dbConn)
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

		return nil
	})

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		// Wireops handles first-run setup in the frontend, so skip the
		// PocketBase installer URL/banner and keep the user on /setup.
		e.InstallerFunc = nil
		return e.Next()
	})

	dockerClient, err := docker.NewClient()
	if err != nil {
		log.Printf("Warning: could not initialize Docker client: %v", err)
	}

	workerSvc := worker.NewService(app)
	workerServer := worker.NewWorkerServer(app, workerSvc)
	scheduler := wiresync.NewScheduler(app, dockerClient, workerServer)
	jobSched := jobscheduler.NewScheduler(app, workerServer, dataDir)

	var disconnectTimers sync.Map

	workerServer.SetOnConnect(func(workerID string) {
		if timer, loaded := disconnectTimers.LoadAndDelete(workerID); loaded {
			if t, ok := timer.(*time.Timer); ok {
				t.Stop()
				log.Printf("[jobscheduler] worker=%s reconnected, cancelled pending disconnect handler", workerID)
			}
		}
		scheduler.TriggerPendingReconciles(workerID)
	})

	workerServer.SetOnDisconnect(func(workerID string) {
		gracePeriod := 10 * time.Second
		if val := os.Getenv("WORKER_DISCONNECT_GRACE_PERIOD"); val != "" {
			if d, err := time.ParseDuration(val); err == nil {
				gracePeriod = d
			} else if secs, err := strconv.Atoi(val); err == nil {
				gracePeriod = time.Duration(secs) * time.Second
			}
		}

		log.Printf("[jobscheduler] worker=%s disconnected, scheduling disconnect processing in %v", workerID, gracePeriod)

		if oldTimer, loaded := disconnectTimers.LoadAndDelete(workerID); loaded {
			if t, ok := oldTimer.(*time.Timer); ok {
				t.Stop()
			}
		}

		timer := time.AfterFunc(gracePeriod, func() {
			disconnectTimers.Delete(workerID)

			if workerServer.IsConnected(workerID) {
				log.Printf("[jobscheduler] worker=%s is connected at timer firing time, skipping disconnect processing", workerID)
				return
			}

			log.Printf("[jobscheduler] grace period expired for worker=%s, processing disconnect", workerID)
			if err := jobSched.HandleWorkerDisconnect(workerID); err != nil {
				log.Printf("[jobscheduler] worker disconnect handle error worker=%s: %v", workerID, err)
			}
		})
		disconnectTimers.Store(workerID, timer)
	})

	workerServer.SetOnHeartbeat(func(workerID string, activeIDs []string) {
		if err := jobSched.ReconcileActiveJobs(workerID, activeIDs); err != nil {
			log.Printf("[jobscheduler] worker heartbeat reconcile error worker=%s: %v", workerID, err)
		}
	})

	workerServer.SetOnJobCompleted(func(msg protocol.JobCompletedMessage) {
		jobSched.HandleJobCompleted(msg)
	})

	go func() {
		addr := ":8443"
		if port := os.Getenv("TLS_WORKER_PORT"); port != "" {
			addr = ":" + strings.TrimPrefix(port, ":")
		}

		tlsCfg, tlsErr := wiretls.BuildServerTLSConfig()
		if tlsErr != nil {
			log.Fatalf("Fatal: failed to build TLS config for worker server: %v", tlsErr)
		}

		if tlsCfg != nil {
			certFile := os.Getenv("TLS_CERT_FILE")
			if certFile != "" {
				log.Printf("[TLS] Worker server TLS enabled using certificate: %s", certFile)
			} else {
				log.Printf("[TLS] Worker server TLS enabled with self-signed certificate (workers need WORKER_TLS_SKIP_VERIFY=true)")
			}
		}

		if err := workerServer.Start(addr, tlsCfg); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Fatal: worker server failed: %v", err)
		}
	}()

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		log.Println("[db] Database migrations completed successfully.")

		configureOIDC(app)
		syncSuperusers(app)

		// OIDC client secret lives only in OIDC_CLIENT_SECRET; inject into the collection cache
		// for OAuth handlers, then reload the cache so it is not retained for other routes.
		// Suppress HTTP access logs (activity logger) unless running in DEBUG mode.
		// PocketBase's native SkipSuccessActivityLog middleware prevents successful
		// requests from being recorded — the equivalent of an access log filter.
		if !logger.IsDebug() {
			se.Router.Bind(apis.SkipSuccessActivityLog())
		}

		se.Router.BindFunc(hooks.SSOUsersOAuthRuntimeMiddleware(app))

		// Configure CORS middleware based on APP_URL
		se.Router.BindFunc(configureCORSMiddleware)
		se.Router.BindFunc(wireauth.APIKeyMiddleware(app))
		se.Router.BindFunc(audit.CustomRouteMiddleware(app))

		routes.RegisterSetupRoutes(se.Router, app)
		routes.RegisterMetricsRoutes(se.Router, app, workerServer)
		routes.Register(se.Router, app, scheduler, dockerClient, workerServer)
		routes.RegisterWorkerRoutes(se.Router, app, workerSvc, workerServer, workerServer)
		routes.RegisterJobRoutes(se.Router, app, jobSched)
		routes.RegisterAuditRoutes(se.Router, app)
		routes.RegisterAuthRoutes(se.Router, app)
		routes.RegisterServiceAccountRoutes(se.Router, app)
		routes.RegisterSSOGroupRoleRoutes(se.Router, app)

		se.Router.GET("/{path...}", apis.Static(os.DirFS("./pb_public"), false))

		if err := wiresync.RecoverOrphanState(app); err != nil {
			log.Printf("Warning: orphan state recovery error: %v", err)
		}

		if err := scheduler.Start(); err != nil {
			log.Printf("Warning: scheduler start error: %v", err)
		}
		jobSched.Start()

		// Mark job_runs stuck in "running" for more than 1 hour as "forgotten".
		app.Cron().Add("job_forgotten_sweep", "*/5 * * * *", func() {
			if err := jobSched.MarkForgottenRuns(); err != nil {
				log.Printf("[jobscheduler] forgotten run sweep error: %v", err)
			}
		})

		app.Cron().Add("worker_token_expiry", "*/5 * * * *", func() {
			if err := workerSvc.ExpireStagingTokens(); err != nil {
				log.Printf("[WORKER] Failed to expire staging tokens: %v", err)
			}
		})

		app.Cron().Add("retention_cleanup", "0 3 * * *", func() {
			if err := audit.PurgeExpired(app); err != nil {
				log.Printf("[cron] retention_cleanup failed: %v", err)
			}
		})

		return se.Next()
	})

	hooks.Register(app, scheduler, jobSched)

	syncHandler := func(e *core.RecordEvent) error {
		syncSuperusers(app)
		return e.Next()
	}
	app.OnRecordAfterCreateSuccess("users").BindFunc(syncHandler)
	app.OnRecordAfterUpdateSuccess("users").BindFunc(syncHandler)
	app.OnRecordAfterDeleteSuccess("users").BindFunc(syncHandler)

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

// syncSuperusers mirrors admin users into the _superusers table to ensure
// their PocketBase internal UI access credentials match WireOps.
func syncSuperusers(app core.App) {
	adminUsers, err := app.FindAllRecords("users", dbx.HashExp{"role": "admin", "disabled": false})
	if err != nil {
		log.Printf("[boot] warning: failed to fetch admin users for sync: %v", err)
		return
	}

	superCol, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		return
	}

	validAdmins := make(map[string]*core.Record)
	for _, u := range adminUsers {
		email := u.GetString("email")
		if email != "" {
			validAdmins[email] = u
		}
	}

	currentSuperusers, err := app.FindAllRecords(core.CollectionNameSuperusers)
	if err == nil {
		for _, su := range currentSuperusers {
			email := su.GetString("email")
			if _, ok := validAdmins[email]; !ok {
				if err := app.Delete(su); err != nil {
					log.Printf("[boot] warning: failed to prune superuser %s: %v", email, err)
				} else {
					log.Printf("[boot] pruned superuser %s (no longer an active admin)", email)
				}
			}
		}
	}

	for _, u := range validAdmins {
		email := u.GetString("email")
		superRecord, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, email)
		if err != nil {
			superRecord = core.NewRecord(superCol)
			superRecord.Set("email", email)
		}

		if superRecord.GetString("passwordHash") != u.GetString("passwordHash") || superRecord.GetString("tokenKey") != u.GetString("tokenKey") {
			superRecord.Set("passwordHash", u.GetString("passwordHash"))
			superRecord.Set("tokenKey", u.GetString("tokenKey"))

			if err := app.SaveNoValidate(superRecord); err != nil {
				if strings.Contains(err.Error(), "Invalid or duplicated auth record id") {
					if superRecord.Id != "" && !superRecord.IsNew() {
						app.Delete(superRecord)
						superRecord = core.NewRecord(superCol)
						superRecord.Set("email", email)
						superRecord.Set("passwordHash", u.GetString("passwordHash"))
						superRecord.Set("tokenKey", u.GetString("tokenKey"))
						if err2 := app.SaveNoValidate(superRecord); err2 != nil {
							log.Printf("[boot] warning: failed to recreate superuser %s: %v", email, err2)
						} else {
							log.Printf("[boot] resolved duplicated ID for superuser %s", email)
						}
					} else {
						log.Printf("[boot] warning: failed to sync superuser %s: %v", email, err)
					}
				} else {
					log.Printf("[boot] warning: failed to sync superuser %s: %v", email, err)
				}
			}
		}
	}
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
