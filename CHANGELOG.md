# Changelog

User-visible changes are recorded here. The project follows semantic versioning
from `v1.0.0`; pre-1.0 releases may include breaking configuration or protocol
changes in a minor release.

## Unreleased

### Added

- Registry credentials are now selected automatically from the exact registry
  hosts referenced by a stack's final Compose images, including image tags,
  digests, render-time overrides, explicit ports, and Docker Hub's implicit
  host. A registry with no matching credential continues anonymously; changing
  a credential applies on the next deploy and does not restart stacks.
- File-managed stacks can now refresh their committed definition explicitly
  with `POST /api/custom/stacks/{id}/resync-definition` or the MCP
  `resync_stack_definition` tool. This re-fetches and validates the stack's
  `x-wireops` block or `wireops.yaml`, applies changed Git-managed settings
  such as `timeout`, and returns a field-by-field diff without deploying.
  Pass `?preview=true` (or `preview` in MCP) to get that diff without saving —
  worth doing first, since `worker_tags` is one of the fields it applies and
  changing that moves the stack to another node.
- A git-driven sync now re-reads `timeout` and `sync.interval` from the
  committed definition on every reconcile — before the skip gates, since the
  renderer strips `x-wireops` and editing only those keys leaves the rendered
  compose unchanged — so editing either in git takes effect on the next poll
  instead of only when the stack is recreated.
  Only those two fields are refreshed automatically: they change how a deploy
  is scheduled, while `worker_tags` and `compose_path` change *where* and
  *what* is deployed and so stay with the explicit resync route.
- The MCP server now exposes the `operator` actions, not only reads: deploy,
  force redeploy, rollback, pause/resume, render-time overrides, container
  restart/stop, transfer, delete, env var writes, stack import, stack creation
  from a repository compose file, repository and scheduled-job management, job
  runs, worker revocation, and one-shot `exec_in_container`, which reports the
  command's output and its exit code. Authorization is unchanged and still
  the server's: an API key belongs to a service account,
  which can only be `viewer` or `operator`, so a viewer-scoped key keeps
  getting `403` for every one of them and an instance that does not run the MCP
  container is unaffected.
- Every MCP tool now carries the protocol's read-only/destructive/idempotent
  hints, so a client can tell which calls are safe unattended, and the
  irreversible ones require a `confirm` field repeating the resource's exact
  name (stack name, worker hostname, repository name, job name) before any
  write leaves the process.
- Per-service **Restart All** and **Stop All** controls, plus multi-select
  restart/stop operations for arbitrary containers in a stack.
- `GET /api/custom/me` reports the calling user's or API key's role, the
  capabilities that role actually grants, and the server's version and default
  poll interval. It needs no permission beyond being authenticated, so a client
  (an agent through the MCP server, above all) can find out what it may do
  instead of discovering it one `403` at a time.
- A deploy's `403` now names the capability it needed, the minimum role that
  capability requires, and the role the caller actually has — on every
  capability-gated route, not just deploys.
- MCP: a `whoami` tool reporting the calling key's role, what that role is
  allowed to do, and the server's version and default poll interval. Every
  permission error from the MCP server now points at it and, where the server
  said so, names the capability that was missing.
- MCP: every tool that takes a stack, repository, worker or job id also accepts
  its exact name (or hostname). An ambiguous name is reported with the
  candidate ids rather than resolved by guessing, and an id costs no extra
  request.
- MCP: `deploy_stack`, `force_redeploy_stack` and `rollback_stack` take
  `wait: true`, which watches the server's log stream until the reconcile
  reports a terminal status, the wait deadline passes, or the stream ends.
  On a terminal status it returns the outcome — final sync status, stack
  status and deploy log — in one call, instead of leaving the caller to poll;
  it recognizes every way a reconcile can end, including the one where there
  was nothing to deploy. Otherwise `completed` is false and the caller polls:
  `timed_out` tells a deadline apart from a stream that ended early (a server
  restart, an idle proxy), and neither is reported as a failed deploy. A
  stream that becomes unreadable after the trigger landed says so and says
  not to retry, since the deploy is already running. An outcome that may
  belong to a reconcile which was already running when the trigger was sent
  is flagged as such. It reads `sync_logs` once before the trigger and once
  after the wait, and never polls in between.
- MCP: `list_stacks` and `list_repos` take filters (status, name substring,
  and for stacks the repository and worker), pages, and a field projection,
  and return a compact default projection with the repository and worker
  expanded to their names. `list_jobs` and `list_workers` take name/status
  filters and a limit. A stack or repository record now also reports
  `effective_sync_interval_seconds`, which resolves what a stored `0` means.
- MCP: `list_secrets` also lists the names of the *non*-secret env vars, so an
  empty secret list can no longer be read as "this stack has no env vars".
  `get_stack_compose` names any `${VAR}` the stack does not define — wireops
  deploys with `--no-interpolate`, so the worker silently substitutes an empty
  string for it. The repository file listings mark each compose/job file
  `registered` and, when it is, name the stack or job that claims it.
- MCP: `MCP_TOOLS` selects which tool groups a deployment registers
  (`read`, `write`, `exec`, or `all`; default unchanged). An instance used only
  for diagnosis registers the read group and halves what every session pays to
  list the tools.
- Running a one-shot command in a container is now a real operation rather
  than an emulated one: `POST /api/custom/stacks/{id}/exec` dispatches a
  non-interactive `docker exec` to the worker and returns separated `stdout`
  and `stderr` with the container runtime's own exit code. It needs the same
  permission as the terminal (`use_terminal`) and is recorded in the audit log
  with the command it ran.
- MCP: `exec_in_container` uses that route, so it reports stdout, stderr and
  the exit code as distinct fields, and accepts the command as an `argv` array
  for an image with no shell.
- On-demand stack health (`GET /api/custom/stacks/{id}/health`) and a paged
  fleet inventory (`GET /api/custom/inventory`, also `get_inventory` in MCP)
  now report the deployed revision's live Docker health without starting a
  sync or a background monitor. Inventory isolates worker failures and marks
  incomplete data instead of discarding the rest of the fleet.
- Resource inventory reports named volumes, bind mounts and external networks
  with explicit `partial`/`warnings`; an empty inventory now means confirmed
  empty. Workers advertise `resource_inventory_v1` to confirm the complete
  collection contract.
- Container logs accept server-validated `since`, `until` and `pattern`, bound
  both inspected and returned bytes, and return applied filters plus
  truncation metadata. `container_logs_filter_v1` protects time-window
  filtering during mixed worker upgrades.
- Stack diagnostics now carry `last_error_commit`, without guessing a commit
  for historical failures; a successful deploy clears it with the other
  `last_error_*` fields.

### Changed

- `POST /api/custom/stacks/{id}/resync-definition` now requires
  `operate_stacks` rather than `manage_repositories`. It mutates one stack's
  deploy behaviour, which is the same blast radius as a deploy, and takes its
  input from the repository rather than from the caller; both capabilities sit
  at the `operator` role, so this narrows the requirement without changing the
  role tier.
- MCP stack inputs also accept `stack`; container tools accept
  `container_name`, and logs accept `lines`/`search`. Equal canonical and
  alias values are accepted; conflicting values are rejected. `exec_in_container`
  now returns the worker's `409` for a missing `exec_one_shot_v1` directly and
  never opens, polls or marker-drives a terminal-session fallback.

- Reading a worker's deploy security policy (`GET
  /api/custom/workers/{id}/policy` and `GET /api/custom/settings/worker-policy`)
  now requires the new `view_worker_policy` capability (`operator`) instead of
  `manage_settings` (`admin`). Writing one still requires `manage_settings`.
  Service accounts cap at `operator`, so an API key could previously deploy a
  compose file while being unable to read the policy that decides whether the
  deploy would be rejected.
- The stack log stream (`GET /api/custom/stacks/{id}/stream`) additionally
  emits a named `status` event on each sync status transition, and accepts
  `?history=false` to skip replaying stored output. Existing consumers read
  only the unnamed output frames and are unaffected. A connected stream also
  re-reads the newest sync log every few seconds, so a status transition whose
  broadcast was dropped under load still reaches the client.
- MCP: the long explanations of stack status, init services and the compose
  label/annotation conventions moved out of the tool descriptions. The status
  and service semantics are now reported as a `_notice` on the response that
  actually has that status, and the conventions live in a new
  `wireops://conventions/compose` resource instead of being duplicated in
  `scaffold_stack`'s description and the `scaffold_new_stack` prompt.

### Fixed

- A deploy killed by `deploy_timeout_seconds` no longer leaves the stack record
  describing a host that does not exist. `docker compose up` is not atomic, so
  the containers it had already recreated keep running the new revision while
  `deployed_commit` still named the old one — `status: error` next to a health
  rollup reporting the services up, and only `force_redeploy_stack` able to
  converge it, since `deploy_stack` correctly reported `noop` against the
  already-matching render. A timed-out deploy now asks the worker what is
  actually on the host and, when the containers carry the revision it was
  deploying, records that revision and takes its status from the post-check
  (`active`, or `degraded` when only some services came up). The deploy still
  counts as failed: `last_error_*` says so, and the sync log stays `error`.
  Convergence needs *every* container to agree on that revision: nothing is
  converged when they straddle two revisions, when they are still on the old
  one, when the commit did not move in the first place (so the labels cannot
  show this render was applied), or when the post-check could not verify
  anything. A worker now reports the whole set of revisions it finds, and one
  too old to do so is treated as unable to confirm rather than as agreeing.
- Stacks now carry `last_error_reason` (`timeout`, `dispatch` or `compose`)
  alongside `last_error_category`, so a caller can tell a deploy that ran out
  of time from one that failed on its own terms without matching
  `signal: killed` out of a compose progress log — which an out-of-memory kill
  produces identically. Workers report the timeout as a flag rather than as
  text; an older worker simply reports no reason, as before.
- MCP: a validation error for an argument a tool does not take now names the
  parameter it does. Sending `lines` to a tool that is not
  `get_container_logs` used to come back as
  `unexpected additional properties ["lines"]` with no pointer at `tail`. The
  input schemas stay closed — the fix is to say what the right name is, not to
  start accepting the wrong one.

### Removed

- MCP: the `list_audit_logs`, `list_integrations`, `list_orphans` and
  `get_system_info` tools. All four called admin-only routes, and an API key
  belongs to a service account, which can only be a viewer or an operator — so
  they were listed as available and then failed with `403` every single time.
  The server version and poll interval they were wanted for are in `whoami`
  instead.

### Security

- Rebuild the server, worker, and MCP images on Alpine 3.24; the previous
  Alpine 3.20 base reached end of support in April 2026 and no longer received
  security updates.
- Update the Docker CLI embedded in the server and worker images to 29.x.
- Fail the `govulncheck` CI gate when the scanner itself cannot run (crash or
  unreachable vulnerability database) instead of reporting a clean result.
- Require Go 1.27.2 as the minimum toolchain in `go.mod`. CI and the release
  images already built on 1.27.2, but the module only asked for 1.27, so a
  local build could silently link the 1.27.0 standard library and its
  `crypto/tls`, `net/http` and `os` advisories.
- Build the server, worker, and MCP binaries with Go 1.27.3, which clears
  GO-2026-6809: the `crypto/x509` verifier in 1.27.2 kept trusting
  intermediate certificates that have since been distrusted, and every TLS
  client in wireops (git over HTTPS, registry auth, webhook and ntfy
  delivery, the MCP process calling the server) reached it. Also run the
  `mcp` CI job's `govulncheck` through `scripts/govulncheck.sh`, like the
  server and worker jobs, so it honours `.govulncheck-allowlist.txt` instead
  of failing on an advisory the other two jobs accept.

### Changed

- Build with Go 1.27.2 and update Go and frontend dependencies, including
  PocketBase v0.40.

### Documentation

- Define the pre-1.0 server-worker compatibility policy.
- Add production, upgrade, troubleshooting, disaster-recovery, and security
  guidance.
- Pin the example server and worker images to one configurable release.
- Document Linux data-directory and Docker-socket permissions.

## v0.2.11 — 2026-08-11

### Added

- Worker-authenticated private registry pulls with encrypted registry
  credentials scoped to a stack deploy.
- Registry connection testing and explicit insecure-registry opt-in.
- Dashboard panel and quick-action improvements.

### Security

- Block server-side request forgery, redirect, and DNS-rebinding paths in
  registry connection tests.
- Require modern TLS for the worker registry-authentication preflight unless an
  operator explicitly enables insecure registry behavior.

Full release notes: <https://github.com/wireops/wireops/releases/tag/v0.2.11>

## Earlier releases

Release notes before `v0.2.11` remain available at
<https://github.com/wireops/wireops/releases>. Material upgrade notes will be
backfilled here when they affect the `v1.0` migration path.
