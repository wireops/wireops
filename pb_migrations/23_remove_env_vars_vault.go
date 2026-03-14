package pb_migrations

import (
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(removeVaultEnvVarFields, addVaultEnvVarFields)
}
