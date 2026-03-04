package sync

import (
	"context"
	"log"
	"path/filepath"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/jfxdev/wireops/internal/compose"
)

type StatusWatcher struct {
	app        core.App
	reconciler *Reconciler
}

func NewStatusWatcher(app core.App, reconciler *Reconciler) *StatusWatcher {
	return &StatusWatcher{app: app, reconciler: reconciler}
}

func (w *StatusWatcher) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.refreshAll(ctx)
		}
	}
}

func (w *StatusWatcher) refreshAll(ctx context.Context) {
	if w.reconciler.dockerClient == nil {
		return
	}

	stacks, err := w.app.FindAllRecords("stacks")
	if err != nil {
		log.Printf("[watcher] failed to list stacks: %v", err)
		return
	}

	for _, stack := range stacks {
		stackID := stack.Id
		repoID := stack.GetString("repository")
		workDir := w.stackWorkDir(stack, repoID)
		projectName := compose.ProjectName(workDir)

		statuses, err := compose.GetStackStatus(ctx, w.reconciler.dockerClient.Raw(), projectName)
		if err != nil {
			continue
		}

		collection, err := w.app.FindCollectionByNameOrId("stack_services")
		if err != nil {
			continue
		}

		existing, _ := w.app.FindAllRecords("stack_services",
			dbx.HashExp{"stack": stackID},
		)
		existingMap := make(map[string]*core.Record)
		for _, rec := range existing {
			existingMap[rec.GetString("service_name")] = rec
		}

		now := time.Now().UTC().Format(time.RFC3339)
		for _, s := range statuses {
			var record *core.Record
			if rec, ok := existingMap[s.ServiceName]; ok {
				record = rec
			} else {
				record = core.NewRecord(collection)
				record.Set("stack", stackID)
				record.Set("service_name", s.ServiceName)
			}
			record.Set("status", s.Status)
			record.Set("container_id", s.ContainerID)
			record.Set("last_checked_at", now)
			_ = w.app.Save(record)
		}
	}
}

func (w *StatusWatcher) stackWorkDir(stack *core.Record, repoID string) string {
	if stack.GetString("source_type") == "local" {
		if importPath := stack.GetString("import_path"); importPath != "" {
			return filepath.Dir(importPath)
		}
	}
	workspace := filepath.Join(w.app.DataDir(), "repositories")
	composePath := stack.GetString("compose_path")
	base := filepath.Join(workspace, repoID)
	if composePath != "" && composePath != "." {
		return filepath.Join(base, composePath)
	}
	return base
}
