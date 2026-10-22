# Changelog

User-visible changes are recorded here. The project follows semantic versioning
from `v1.0.0`; pre-1.0 releases may include breaking configuration or protocol
changes in a minor release.

## Unreleased

### Added

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
  for an image with no shell. Against a worker too old to support it the tool
  falls back to the previous terminal-session method and says so in the
  response, where stdout and stderr are necessarily interleaved.

### Changed

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
