package routes

import (
	"os"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/logstream"
	"github.com/wireops/wireops/internal/sync"
	"github.com/wireops/wireops/internal/termstream"
)

const OfflineWorkerMsg = "worker '%s' is offline"

func Register(r *router.Router[*core.RequestEvent], app core.App, scheduler *sync.Scheduler, workerSvc sync.WorkerDispatcher, logBroker *logstream.Broker, termBroker *termstream.Broker) {
	registrar := routeRegistrar{
		r:          r,
		app:        app,
		scheduler:  scheduler,
		workerSvc:  workerSvc,
		logBroker:  logBroker,
		termBroker: termBroker,
	}

	registrar.registerStackTriggerRoutes()
	registrar.registerBackupRoutes()
	registrar.registerStreamRoutes()
	registrar.registerStackInspectionRoutes()
	registrar.registerContainerReadRoutes()
	registrar.registerRepositoryRoutes()
	registrar.registerCredentialRoutes()
	registrar.registerRegistryCredentialRoutes()
	registrar.registerGitProviderRoutes()
	registrar.registerStackComposeRoute()
	registrar.registerStackRevisionRoute()
	registrar.registerStackDependencyGraphRoute()
	registrar.registerSopsRoutes()
	registrar.registerEnvVarRoutes()
	registrar.registerContainerActionRoutes()
	registrar.registerStackDeleteRoute()
	registrar.registerStackTransferRoute()
	registrar.registerMigratePreviewRoute()
	registrar.registerMigrateRoute()
	registrar.registerSystemRoutes()
	registrar.registerImportRoutes()
	registrar.registerCreateFromWireopsRoute()
	registrar.registerLintRoutes()
	registrar.registerTerminalRoutes()
	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	registrar.registerIntegrationRoutes(secretKey)
	registrar.registerVaultBrowseRoutes(secretKey)
	registrar.registerInfisicalBrowseRoutes(secretKey)

	RegisterUserRoutes(r, app)
}
