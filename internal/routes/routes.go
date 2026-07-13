package routes

import (
	"os"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/sync"
)

const OfflineWorkerMsg = "worker '%s' is offline"

func Register(r *router.Router[*core.RequestEvent], app core.App, scheduler *sync.Scheduler, workerSvc sync.WorkerDispatcher) {
	registrar := routeRegistrar{
		r:         r,
		app:       app,
		scheduler: scheduler,
		workerSvc: workerSvc,
	}

	registrar.registerStackTriggerRoutes()
	registrar.registerBackupAndStreamRoutes()
	registrar.registerStackInspectionRoutes()
	registrar.registerContainerReadRoutes()
	registrar.registerRepositoryRoutes()
	registrar.registerCredentialRoutes()
	registrar.registerStackComposeRoute()
	registrar.registerContainerActionRoutes()
	registrar.registerStackDeleteRoute()
	registrar.registerStackTransferRoute()
	registrar.registerSystemRoutes()
	registrar.registerImportRoutes()
	registrar.registerCreateFromWireopsRoute()
	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	registrar.registerIntegrationRoutes(secretKey)
	registrar.registerVaultBrowseRoutes()
	registrar.registerInfisicalBrowseRoutes()

	RegisterUserRoutes(r, app)
}
