package pb_migrations

import (
	"log"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		records, err := app.FindAllRecords("scheduled_jobs")
		if err != nil {
			return err
		}

		updated := 0
		for _, rec := range records {
			if rec.GetString("name") != "" {
				continue
			}
			rec.Set("name", migrationScheduledJobName(rec.GetString("job_file"), rec.Id))
			if err := app.Save(rec); err != nil {
				return err
			}
			updated++
		}

		log.Printf("[MIGRATE] Backfilled names for %d scheduled_jobs record(s)", updated)
		return nil
	}, func(app core.App) error {
		return nil
	})
}

func migrationScheduledJobName(jobFile, id string) string {
	name := strings.TrimSuffix(filepath.Base(jobFile), filepath.Ext(jobFile))
	if name == "" || name == "." {
		name = "job_" + id
	}
	return sanitizeMigrationScheduledJobName(name, id)
}

func sanitizeMigrationScheduledJobName(name, id string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	previousSeparator := false
	for _, r := range name {
		allowed := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == ' '
		if allowed {
			b.WriteRune(r)
			previousSeparator = r == '-' || r == ' '
			continue
		}
		if !previousSeparator {
			b.WriteRune('-')
			previousSeparator = true
		}
	}

	sanitized := strings.Trim(b.String(), " -_")
	if sanitized == "" {
		sanitized = "job_" + id
	}
	return sanitized
}
