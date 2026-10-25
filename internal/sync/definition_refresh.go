package sync

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/manifest"
	"github.com/wireops/wireops/internal/safepath"
	"github.com/wireops/wireops/internal/stackdef"
)

// scheduleChange records one field a reconcile re-read from the committed
// definition, for the sync log and the reconcile log line.
type scheduleChange struct {
	Field string
	From  int
	To    int
}

func (c scheduleChange) String() string {
	return fmt.Sprintf("%s %d -> %d", c.Field, c.From, c.To)
}

// refreshScheduleUnderRepoLock runs refreshScheduleFromDefinition with the
// repository's read lock held, and logs whichever of the three outcomes
// happened. The lock is the point of the wrapper: the definition is read out
// of the shared checkout (workspace/<repoID>), which every git fetch rewrites,
// and this runs ahead of the render phase's own read lock rather than under
// it. It is released again here instead of being held until the render — an
// RWMutex gives writers priority, so re-entering it while a queued fetch waits
// would deadlock.
func (r *Reconciler) refreshScheduleUnderRepoLock(stack *core.Record, repoID, stackID string) {
	repoRead := r.repoMutex(repoID)
	repoRead.RLock()
	defer repoRead.RUnlock()

	changes, err := r.refreshScheduleFromDefinition(stack, repoID)
	switch {
	case err != nil:
		log.Printf("[reconciler] stack %s: keeping stored schedule, committed definition unreadable: %v", stackID, err)
	case len(changes) > 0:
		log.Printf("[reconciler] stack %s: refreshed schedule from git (%s)", stackID, formatScheduleChanges(changes))
	}
}

// refreshScheduleFromDefinition re-reads the stack's committed wireops
// definition and applies the fields that only change *how* a deploy is
// scheduled: deploy_timeout_seconds and sync_interval_seconds.
//
// It deliberately does not touch the rest of wireopsManagedStackFields.
// worker_tags would relocate a running stack to another node because someone
// edited a file, and compose_path/compose_file would re-point it at different
// sources — neither should happen without an operator asking. Those stay with
// the explicit resync-definition route, which reports a diff. The asymmetry is
// the whole reason the immutability rule exists; this is the safe half of it.
//
// The caller must hold the stack lock: the save has to be ordered against the
// deploy that reads these values. Returns the applied changes, which is empty
// when the definition already matches.
func (r *Reconciler) refreshScheduleFromDefinition(stack *core.Record, repoID string) ([]scheduleChange, error) {
	source := stack.GetString("config_source")
	if !scheduleRefreshApplies(source) {
		return nil, nil
	}

	def, err := r.parseStackDefinition(stack, source, repoID)
	if err != nil {
		return nil, err
	}

	changes := scheduleChangesFor(stack, def)
	if len(changes) == 0 {
		return nil, nil
	}

	// The immutability hook rejects generic edits to these fields; this bypass
	// applies only after the committed definition was read and parsed, and is
	// cleared as soon as the save returns.
	if err := stackdef.WithBypass(stack.Id, func() error {
		return r.app.Save(stack)
	}); err != nil {
		return nil, fmt.Errorf("save refreshed schedule: %w", err)
	}
	return changes, nil
}

// scheduleRefreshApplies reports whether a stack's schedule is owned by a file
// in git. A stack created through the UI keeps whatever it was given there.
func scheduleRefreshApplies(configSource string) bool {
	return configSource == "wireops_file" || configSource == "compose_embedded"
}

// scheduleChangesFor sets the schedule fields that differ from def on stack and
// returns what it changed. It is separate from the IO above so the convergence
// rule itself is testable without a live app.
//
// A definition that parses but declares neither key leaves both at 0, which
// deployUpTimeoutSeconds and the scheduler already read as "use the global
// default". That is a real value to converge on, not a missing one.
func scheduleChangesFor(stack *core.Record, def *manifest.Definition) []scheduleChange {
	candidates := []scheduleChange{
		{Field: "deploy_timeout_seconds", To: def.DeployTimeoutSeconds},
		{Field: "sync_interval_seconds", To: def.SyncIntervalSeconds},
	}

	changes := make([]scheduleChange, 0, len(candidates))
	for _, candidate := range candidates {
		candidate.From = stack.GetInt(candidate.Field)
		if candidate.From == candidate.To {
			continue
		}
		stack.Set(candidate.Field, candidate.To)
		changes = append(changes, candidate)
	}
	return changes
}

// parseStackDefinition reads the definition from the already-fetched
// workspace, using the stack's persisted source identity rather than anything
// derived at call time.
func (r *Reconciler) parseStackDefinition(stack *core.Record, source, repoID string) (*manifest.Definition, error) {
	if source == "wireops_file" {
		wireopsFile := stack.GetString("wireops_file_path")
		if wireopsFile == "" {
			return nil, fmt.Errorf("stack has no wireops_file_path")
		}
		def, err := manifest.ParseWireopsFile(r.reposWorkspace(), repoID, wireopsFile)
		if err != nil {
			return nil, err
		}
		return def, nil
	}

	// Resolved here rather than taken from the caller: this runs before the
	// render phase computes them, and it must not reuse resolveComposeFile,
	// whose failure path marks the stack as errored.
	workDir, err := r.stackWorkDir(stack, repoID)
	if err != nil {
		return nil, err
	}
	composeFile := stack.GetString("compose_file")
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}
	if err := safepath.ValidateComposeFile(composeFile); err != nil {
		return nil, err
	}
	repoRoot := filepath.Join(r.reposWorkspace(), repoID)
	data, _, err := compose.ReadFile(repoRoot, workDir, composeFile, config.GetComposeMaxBytes())
	if err != nil {
		return nil, err
	}
	def, err := manifest.ParseComposeManifest(data)
	if err != nil {
		return nil, err
	}
	if def == nil {
		// The stack was created from a compose file that had an x-wireops
		// block; if it no longer has one there is nothing to converge on, and
		// silently resetting both fields to zero would widen the deploy budget.
		return nil, fmt.Errorf("compose file no longer has an %s block", manifest.ExtensionKey)
	}
	return def, nil
}

// formatScheduleChanges renders changes for a single log line.
func formatScheduleChanges(changes []scheduleChange) string {
	parts := make([]string, 0, len(changes))
	for _, change := range changes {
		parts = append(parts, change.String())
	}
	return strings.Join(parts, ", ")
}
