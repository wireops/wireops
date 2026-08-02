package configfiles

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// TrackedFile is a resolved config file plus where it was mounted, recorded
// in PocketBase for UI/drift visibility.
type TrackedFile struct {
	Name       string
	SourcePath string
	Target     string
	Checksum   string
}

// UpsertStackConfigFiles replaces the stack_config_files rows for stackID
// with files. Delete-then-recreate keeps the set exact, so a config removed
// from wireops.yaml doesn't leave a stale row behind.
func UpsertStackConfigFiles(app core.App, stackID string, files []TrackedFile) error {
	return upsertTrackedFiles(app, "stack_config_files", "stack", stackID, files)
}

// UpsertJobConfigFiles replaces the job_config_files rows for jobID with
// files, same delete-then-recreate semantics as UpsertStackConfigFiles.
func UpsertJobConfigFiles(app core.App, jobID string, files []TrackedFile) error {
	return upsertTrackedFiles(app, "job_config_files", "job", jobID, files)
}

func upsertTrackedFiles(app core.App, collectionName, targetField, targetID string, files []TrackedFile) error {
	collection, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		return err
	}

	existing, err := app.FindAllRecords(collectionName, dbx.HashExp{targetField: targetID})
	if err != nil {
		return err
	}
	for _, rec := range existing {
		if err := app.Delete(rec); err != nil {
			return err
		}
	}

	for _, f := range files {
		record := core.NewRecord(collection)
		record.Set(targetField, targetID)
		record.Set("name", f.Name)
		record.Set("source_path", f.SourcePath)
		record.Set("target", f.Target)
		record.Set("checksum", f.Checksum)
		if err := app.Save(record); err != nil {
			return err
		}
	}

	return nil
}
