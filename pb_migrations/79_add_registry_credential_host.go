package pb_migrations

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/wireops/wireops/internal/registry"
)

const registryCredentialsHostIndexSQL = "CREATE UNIQUE INDEX IF NOT EXISTS idx_registry_credentials_registry_host_unique ON registry_credentials (registry_host)"

// registry_host is a server-derived canonical hostname (plus an optional
// port) for a registry credential. The unique index prevents two equivalent
// URLs, such as HTTPS://REGISTRY.EXAMPLE.COM/v2 and registry.example.com,
// from making deploy-time credential selection ambiguous.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("registry_credentials")
		if err != nil {
			return err
		}
		if col.Fields.GetByName("registry_host") == nil {
			col.Fields.Add(&core.TextField{Name: "registry_host"})
			if err := app.Save(col); err != nil {
				return err
			}
		}

		records, err := app.FindAllRecords("registry_credentials")
		if err != nil {
			return err
		}
		byHost := make(map[string][]*core.Record)
		for _, record := range records {
			host := registry.NormalizeRegistryHost(record.GetString("registry_url"))
			if host == "" {
				return fmt.Errorf("cannot derive registry host for credential %s", record.Id)
			}
			byHost[host] = append(byHost[host], record)
		}

		hosts := make([]string, 0, len(byHost))
		for host := range byHost {
			hosts = append(hosts, host)
		}
		sort.Strings(hosts)
		for _, host := range hosts {
			duplicates := byHost[host]
			if len(duplicates) < 2 {
				continue
			}
			labels := make([]string, 0, len(duplicates))
			for _, duplicate := range duplicates {
				label := duplicate.Id
				if name := strings.TrimSpace(duplicate.GetString("name")); name != "" {
					label += " (" + name + ")"
				}
				labels = append(labels, label)
			}
			return fmt.Errorf("cannot add unique registry credential host index: registry %q has %d credentials (%s); consolidate them before restarting", host, len(duplicates), strings.Join(labels, ", "))
		}

		for host, hostRecords := range byHost {
			for _, record := range hostRecords {
				if _, err := app.DB().Update("registry_credentials", dbx.Params{"registry_host": host}, dbx.HashExp{"id": record.Id}).Execute(); err != nil {
					return fmt.Errorf("store registry host for credential %s: %w", record.Id, err)
				}
			}
		}

		for _, index := range col.Indexes {
			if index == registryCredentialsHostIndexSQL {
				return nil
			}
		}
		col.Indexes = append(col.Indexes, registryCredentialsHostIndexSQL)
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("registry_credentials")
		if err != nil {
			return err
		}
		for i := len(col.Indexes) - 1; i >= 0; i-- {
			if col.Indexes[i] == registryCredentialsHostIndexSQL {
				col.Indexes = append(col.Indexes[:i], col.Indexes[i+1:]...)
			}
		}
		col.Fields.RemoveByName("registry_host")
		return app.Save(col)
	})
}
