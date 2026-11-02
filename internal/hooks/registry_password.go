package hooks

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

const registryPasswordRequestKey = "wireops.registryCredentialPassword"

type registryPasswordInput struct{ value string }

// PocketBase removes schema-hidden fields before triggering record request
// hooks. Capture only the registry password before that filtering, then restore
// it after the normal collection access checks and before the encrypted save.
// Keeping the schema hidden also preserves filter/sort and realtime protections.
func registerRegistryCredentialPasswordInput(app core.App) {
	app.OnServe().BindFunc(func(event *core.ServeEvent) error {
		collection, err := app.FindCollectionByNameOrId("registry_credentials")
		if err != nil {
			return err
		}
		event.Router.BindFunc(func(e *core.RequestEvent) error {
			if e.Request.Method != http.MethodPost && e.Request.Method != http.MethodPatch {
				return e.Next()
			}
			ref := e.Request.PathValue("collection")
			if ref != collection.Name && ref != collection.Id {
				return e.Next()
			}
			info, err := e.RequestInfo()
			if err != nil {
				return err
			}
			if raw, present := info.Body["password"]; present {
				value, ok := raw.(string)
				if !ok {
					return e.BadRequestError("Registry credential password must be a string.", nil)
				}
				e.Set(registryPasswordRequestKey, registryPasswordInput{value: value})
			}
			return e.Next()
		})
		return event.Next()
	})

	apply := func(e *core.RecordRequestEvent) error {
		if input, ok := e.Get(registryPasswordRequestKey).(registryPasswordInput); ok {
			e.Record.Set("password", input.value)
		}
		return e.Next()
	}
	app.OnRecordCreateRequest("registry_credentials").BindFunc(apply)
	app.OnRecordUpdateRequest("registry_credentials").BindFunc(apply)
}
