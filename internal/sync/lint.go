package sync

import (
	"context"
	"log"
	"time"

	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/constants"
	"github.com/wireops/wireops/internal/lint"
	"github.com/wireops/wireops/internal/policy"
)

// lintResult is one compose lint run inside a reconcile, held until the
// sync_logs row exists and its sync_log_phases row can be written.
type lintResult struct {
	report   lint.Report
	started  time.Time
	duration int64
	// err is set when the lint could not run at all (compose config failed to
	// resolve, worker policy unreadable). It never fails the reconcile — the
	// render step right after runs the same `docker compose config` and will
	// surface a real problem with its own error.
	err error
}

// detail renders the phase detail line shown in the deploy timeline.
func (l lintResult) detail() string {
	if l.err != nil {
		return "lint skipped: " + l.err.Error()
	}
	return l.report.Summary()
}

// recordLintPhase writes the lint phase's sync_log_phases row.
//
// The status is always success or skipped, never error: lint findings —
// including the error-severity ones mirrored from the worker policy — do not
// fail a deploy, and a red step on the timeline would say they did. The
// counts live in the detail line instead.
func recordLintPhase(pt *phaseTracker, res lintResult) {
	if res.err != nil {
		_ = pt.recordSkipped(constants.PhaseLint, res.detail())
		return
	}
	_ = pt.recordCompleted(constants.PhaseLint, constants.PhaseStatusSuccess, res.started, res.duration, res.detail())
}

// lintCompose runs the static compose checks for a stack about to be
// reconciled, between the git fetch and the render.
//
// It is advisory and never decides whether the deploy proceeds. Policy
// enforcement stays in the renderer, which validates the config after applying
// render overrides — lint runs before that and would otherwise reject a stack
// whose override fixes the very violation it flagged.
//
// Running it here rather than reusing the renderer's own `docker compose
// config` output costs one extra CLI invocation (a client-side parse, no
// daemon) and buys a report of the file as committed, before wireops injects
// its own labels into it.
func (r *Reconciler) lintCompose(ctx context.Context, workDir, composeFile, workerID string, envVars []string) lintResult {
	res := lintResult{started: time.Now()}
	defer func() { res.duration = time.Since(res.started).Milliseconds() }()

	configOut, err := compose.Config(ctx, compose.ConfigOptions{
		WorkDir:     workDir,
		ComposeFile: composeFile,
		EnvVars:     envVars,
	}, true)
	if err != nil {
		res.err = err
		return res
	}

	configMap, err := compose.ParseConfigJSON(configOut)
	if err != nil {
		res.err = err
		return res
	}

	// A worker whose policy cannot be read still gets linted, just without the
	// policy-derived findings — losing every advisory check because of an
	// unrelated lookup failure would be a worse trade.
	var wp *policy.WorkerPolicy
	if workerID != "" {
		wp, err = policy.Load(r.app, workerID)
		if err != nil {
			log.Printf("[lint] failed to load worker policy for %s, linting without it: %v", workerID, err)
			wp = nil
		}
	}

	// The repo's own .env is already folded into envVars by the caller
	// (loadEnvVars + SOPS overlay), so this is the full set the worker will
	// have when it resolves the rendered compose file.
	res.report = lint.Run(configMap, lint.Context{
		Policy:  wp,
		EnvKeys: lint.EnvKeysFromPairs(envVars),
	})
	return res
}
