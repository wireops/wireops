// Package constants holds string values shared across packages that would
// otherwise be repeated (and risk drifting) as inline literals — e.g. the
// deploy timeline's phase/status names, which appear in the reconciler, the
// pb_migrations schema, and tests.
package constants

// Deploy timeline phase names (sync_log_phases.phase). Order matters: it is
// the canonical, fixed ordering used to sort a deploy's phases in the UI
// (see DeployPhaseOrder), independent of the order phases were recorded in.
const (
	PhaseGitFetch    = "git_fetch"
	PhaseRender      = "render"
	PhasePolicyCheck = "policy_check"
	PhaseDispatch    = "dispatch"
	PhaseWorkerAck   = "worker_ack"
	PhaseComposeUp   = "compose_up"
	PhasePostCheck   = "post_check"
	PhaseNotify      = "notify"
)

// DeployPhaseOrder is the canonical, fixed-order set of phases every
// deploy-like flow reports against, so the timeline UI can always expect the
// same shape regardless of which flow produced it or which order individual
// phases were actually recorded in.
var DeployPhaseOrder = []string{
	PhaseGitFetch, PhaseRender, PhasePolicyCheck, PhaseDispatch,
	PhaseWorkerAck, PhaseComposeUp, PhasePostCheck, PhaseNotify,
}

// Deploy timeline phase statuses (sync_log_phases.status).
const (
	PhaseStatusRunning = "running"
	PhaseStatusSuccess = "success"
	PhaseStatusError   = "error"
	PhaseStatusSkipped = "skipped"
)
