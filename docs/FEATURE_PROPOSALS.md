# Feature Proposals

This page is not a roadmap and nothing here is scheduled or committed to. It's a parking lot: ideas that have been checked against the code as it actually stands today, so future prioritization does not have to repeat the evaluation work.

Every entry says what already exists and what would actually have to be built, because the gap between the two is the whole cost estimate.

Proposals are grouped into three tiers by cost and risk:

| Tier | Meaning |
|---|---|
| [Tier 1](#tier-1--small-mostly-wiring-existing-parts-together) | Small, low-risk, mostly connecting parts that already exist |
| [Tier 2](#tier-2--real-features-contained-scope) | Real features with contained scope and meaningful implementation cost |
| [Tier 3](#tier-3--strategic) | Strategic, expensive, or in tension with the project's scope |

## Already shipped — don't propose these

Things frequently suggested for a GitOps tool that wireops already does. Check here before writing a proposal:

| Capability | Where |
|---|---|
| Push-to-deploy via Git webhook (per stack, HMAC-signed) | `POST /api/custom/webhook/{id}` — see the [API reference](https://github.com/wireops/wireops/wiki/API-Reference) |
| Post-deploy health verification + restart-loop detection | `internal/sync/postcheck.go` |
| Static compose linting, gating deploys on errors | `internal/lint/` |
| Worker deploy security policies (privileged, host network, docker.sock, allowlists) | `internal/policy/` — see [access control and audit](https://github.com/wireops/wireops/wiki/Access-Control-and-Audit) |
| RBAC with four roles + audit log | `internal/rbac/`, `internal/audit/` |
| Backups, restore, and off-host S3 mirroring | `internal/backup/` — see [disaster recovery](DISASTER_RECOVERY.md) |
| External secret providers (Vault, Infisical) and SOPS+age | `internal/secrets/` |
| Prometheus metrics, including per-deploy-phase timings | `internal/deploymetrics/` — see [observability](https://github.com/wireops/wireops/wiki/Observability) |
| Rollback to a previous commit, force redeploy, stack transfer | See the [API reference](https://github.com/wireops/wireops/wiki/API-Reference) |
| Render-time overrides (image/ports/networks/scale) without a commit | `internal/sync/renderer.go` |
| Read-only MCP server for external AI agents (~27 tools, its own deployable) | `mcp/`, `Dockerfile.mcp` — see [MCP server](https://github.com/wireops/wireops/wiki/MCP-Server) |
| Command palette, keyboard shortcuts, onboarding tour, empty-state CTAs | `frontend/app/components/AppCommandPalette.vue`, `AppOnboardingTour.vue`, `composables/useKeyboard.ts` |
| Error-message parsing with actionable suggestions (git/compose/docker/secrets) | `frontend/app/utils/error-parser.ts` |
| Deploy timeline with per-phase status, live deploy output streaming | `DeployTimeline.vue`, `composables/useDeployStream.ts`, `internal/logstream/` |
| Private registry credentials scoped to a stack deploy | `internal/registry/`, `worker/executor/registry_auth.go` — shipped in `v0.2.11` |
| Bulk `.env` editing/import and copying variables between stacks | `EnvironmentVariablesBulkEditor.vue`, `CopyEnvVarsModal.vue` — shipped in `v0.2.9` |
| Required Compose variable detection in stack flows | `RequiredEnvVarsBanner.vue`, `internal/lint/` |

---

## Tier 1 — small, mostly wiring existing parts together

Each of these reuses machinery that is already built and tested. They're listed roughly in order of value per unit of effort.

### Auto-rollback on failed post-check

**Today:** `internal/sync/postcheck.go` already queries the worker after a deploy, classifies the result as `active` / `degraded` / `error`, and detects containers stuck in a restart loop. It records that status and stops there.

**Missing:** an opt-in flag (per stack, or `deploy.rollback_on_failure` in `wireops.yaml`) that calls the existing `Reconciler.RollbackStack` when the post-check comes back `error`. Both halves already exist — `stack_revisions` holds the previous rendered revision, and rollback is a supported operation with its own deploy timeline handling.

**Why it matters:** turns "the deploy broke at 3am and stayed broken until someone looked" into a self-correcting event. The single highest value-to-effort item on this page.

### More notification events

**Today:** the configurable lifecycle events are `sync.started`, `sync.done`, `sync.error`, and `backup.mirror_error` (`internal/notify/notify.go`). The Webhook, Discord, Slack, and ntfy plugins consume them (see [notifications](https://github.com/wireops/wireops/wiki/Notifications)).

**Missing:** `job.failed`, `worker.offline`, `policy.violation`, and `lint.error`.

**Why it matters:** a scheduled job that fails currently notifies nobody. The delivery plugins are reused unchanged; this is new event constants plus the call sites that fire them.

### Repository-level webhooks, and non-GitHub providers

**Today:** the push webhook is scoped to a single stack (`POST /api/custom/webhook/{id}`), and signature verification only understands GitHub's `X-Hub-Signature-256` header (`internal/webhook/signature.go`).

**Missing:** a repository-level endpoint that fans out to every stack belonging to that repository, and support for GitLab's `X-Gitlab-Token` and Gitea/Forgejo signatures. Today a repository with five stacks needs five separate webhooks configured on the Git host, and only GitHub can sign them — even though Git provider OAuth already supports the others.

### Revision diff viewer

**Today:** `GET /api/custom/stacks/{id}/revisions/{version}` returns the rendered compose YAML for any stored revision.

**Missing:** a diff view in the UI (v*n* against v*n-1*, and a commit-to-commit diff on the sync timeline). The data is already served; this is almost entirely frontend work. The MCP server already ships a `diff_stack_version` tool, so the comparison itself has a reference implementation to copy.

### Per-stack Prometheus metrics

**Today:** `internal/deploymetrics/` aggregates by deploy *phase* (git fetch, render, deploy, …), not by stack.

**Missing:** per-stack gauges — age of the last successful sync, current status, current revision. That's what makes alerts like "stack X hasn't synced in three days" expressible in Prometheus instead of requiring someone to notice in the UI.

### Worker/server version compatibility check

**Today:** the worker reports its version on registration and the server persists it (`internal/worker/service.go`, `workers.version` from migration `50_add_worker_version.go`). The server knows its own version through `internal/buildinfo`.

**Missing:** nothing compares the two. Add a minimum supported worker version, surface an "outdated worker" badge in the worker list, and warn (or refuse, for a hard protocol break) during the WebSocket handshake.

**Why it matters:** `internal/protocol/messages.go` is shared by both sides and does change. A worker several releases behind currently fails in whatever way the specific message mismatch produces, instead of saying so up front. Both data points are already in the database.

### MFA for local accounts

**Today:** local accounts authenticate with a password only. SSO users inherit whatever their identity provider enforces (`internal/oidc/`), so the accounts *without* a second factor are exactly the ones that survive an IdP outage — including the bootstrap admin created at `/setup`.

**Missing:** PocketBase v0.37.4 already ships the whole mechanism (`core/mfa_model.go`, `core/otp_model.go`, plus the auth-flow endpoints); SMTP is already configured for invites and password resets, and `CapManageSecurity` already exists as the admin-side capability. What's needed is enabling MFA on the users collection, the second step in the login screen, and an instance-wide "require MFA for admins" toggle.

**Why it matters:** the panel can trigger deploys on every host the instance manages and read every secret name. Rated Tier 1 because almost none of it is new code — it is wiring a framework feature the project already depends on.

### Make the timezone of a schedule visible

**Today:** the job scheduler runs cron in a configurable location (`internal/jobscheduler/scheduler.go` — `time.LoadLocation`, `cron.WithLocation`). The UI renders a cron expression as readable English through `frontend/app/utils/cron-translator.ts` ("every day at 02:00") and renders timestamps in the *browser's* locale. Neither says which zone anything is in.

**Missing:** the instance timezone shown next to every schedule, a computed "next run" in that zone, and UTC on hover for timestamps.

**Why it matters:** the two clocks are genuinely different — the cron fires in the server's zone, the timestamp beside it renders in the viewer's. A team spread across zones, or anyone whose server runs UTC, currently has no way to read "02:00" correctly off the screen. Pure display work over data that already exists.

### Needs-attention queue on the dashboard

**Today:** the dashboard shows Getting Started, Quick Actions, System Health, and the ten most recent sync events (`RecentSyncActivity.vue`). Everything it shows is chronological or aggregate.

**Missing:** one list of things waiting on a human, assembled from state that is already persisted — stacks in `error` or `degraded`, workers offline, jobs whose last run failed, stacks carrying `secret_error` (migration 60) or the `last_error_*` fields (migration 61), and deploys stuck pending a worker that never came back.

**Why it matters:** today you find out something broke by navigating to it. Every input already exists as a field; nothing joins them into a single answer to "what needs me right now" — which is the first question anyone opens the panel to ask. It also gives the auto-rollback and drift proposals a natural place to report.

---

## Tier 2 — real features, contained scope

### `SECRET_KEY` rotation

**Today:** rotating `SECRET_KEY` is unsupported in the strongest sense — `internal/crypto/canary.go` refuses to boot and says so explicitly ("SECRET_KEY was rotated without re-encrypting existing data"). The canary detects the mistake; nothing repairs it. A leaked key has no recovery path short of recreating every secret by hand.

**Missing:** an offline subcommand (`wireops rotate-key --old <key> --new <key>`) that takes a backup first through `internal/backup`, then walks every AES-GCM ciphertext — `stack_env_vars` secret values, repository credentials, SSH keys, integration tokens, the OIDC client secret — decrypting with the old key and re-encrypting with the new one, and finally reseeds the canary row.

**One design decision it forces:** `service_accounts.key_hash` is an HMAC-SHA256 derived from `SECRET_KEY`, so rotation invalidates every issued API key. Either that is accepted and documented (all service accounts must be re-issued, which also breaks MCP clients), or `key_hash` moves to a dedicated salt in a migration so it stops being coupled to the encryption key at all. The second is the better end state and can ship independently, ahead of rotation itself.

**Why it matters:** every other credential in the system is rotatable; the one protecting all of them is not. This is the widest gap between what the project's security posture claims and what an operator can actually do after an incident.

### Compose-native `secrets:` and `configs:`

**Today:** secrets reach containers as environment variables only (`internal/envvars/`, resolving through `internal/secrets/` — internal, SOPS+age, Vault, Infisical). Nothing reads or rewrites the compose file's top-level `secrets:` / `configs:` blocks.

**Missing:** resolving a `secrets:` entry against the same providers and delivering it to the worker as a file the compose run mounts, with the file living only for the lifetime of that deploy.

**Why it matters:** plenty of images take credentials only as a file (`*_FILE` conventions, TLS keys, service-account JSON), and today the file has to be pre-placed on the host by hand — the same out-of-band state problem as registry credentials above, reached from the other direction. Related but separately deliverable: registry credentials block the pull, this blocks the run.

### Capacity-aware worker placement

**Today:** `stacks.worker` is a relation a human picks. Worker tags already express *intent* (`prod`, `eu-west-1`) and gate which workers are valid targets, but the final choice among valid ones is manual. Meanwhile `worker/telemetry/telemetry.go` already reports CPU and memory read from `/proc` on every heartbeat, and nothing consumes it for scheduling.

**Missing:** assignment to a tag pool rather than a specific worker, with the server choosing among connected workers that satisfy the tags — and re-placing a stack when its worker is gone for good rather than only queueing in `stack_pending_reconciles` for a worker that may never return.

**Scope note:** this is multi-node scheduling and automatic stack re-placement, which is a boundary change from wireops' current single-worker-per-stack, human-picked-target model. The README scopes wireops to single-host/small-fleet `docker compose` GitOps and explicitly points enterprise-scale orchestration, multi-node scheduling, and autoscaling at Kubernetes with Flux or ArgoCD. Treat this proposal as out of the active tiers unless that product boundary is deliberately revisited.

**Why it matters:** tags are already a pool declaration in everything but the last step. It also converts worker loss from "these stacks are stuck pending" into "these stacks moved", which is the behaviour the tag model implies.

### Glances integration — live worker metrics

**Today:** the worker reports a CPU and memory *snapshot* on every heartbeat (`worker/telemetry/telemetry.go`, read from `/proc`), rendered as current values in `WorkerSystemInfoCard.vue` — no per-core detail, no disk or network I/O, no per-container resource usage. Separately, `internal/integrations/` already has a registry of pluggable integrations with per-container actions (Traefik, Caddy, Nginx Proxy Manager, Dozzle) and their own config modals in Settings.

**Missing:** a Glances integration that ties a wireops worker to the Glances instance running on that host. Glances in web-server mode (`glances -w`, port 61208 by default) exposes both a web UI and a REST API (`/api/4/...` in Glances 4 — the path is version-dependent and worth pinning), including a containers plugin that reports per-container CPU and memory. That covers both surfaces the panel wants: richer live host metrics on the worker page, and per-container usage alongside the existing container list.

**The capability shape is ready, but the route is not.** `integrations.ActionProvider` already accepts an `ActionScope`, and `ScopeWorker` is declared. Current callers still populate only `ScopeContainer`, while the four legacy action providers are adapted from `ResolveContainerActions` and intentionally ignore other scopes. Glances therefore needs a worker-scoped route/caller and a native action provider; it does not require another integration interface redesign. Once that caller exists, Netdata, Beszel, or a per-host Grafana dashboard can use the same path.

**Sequencing:** a link-only first cut — "Open in Glances" on the worker page, exactly the pattern Dozzle already uses for containers — is nearly free and validates the worker-to-host mapping before any API polling or embedding work. Do that first, then add the API-backed metrics.

**Credentials:** Glances' web mode is unauthenticated unless started with a password, so plenty of instances sit open on the LAN. If the integration config accepts one, register the key in `sensitiveIntegrationConfigKeys` (`internal/routes/routes_register.go`) so it is masked in API responses, and decide deliberately whether it also belongs in `encryptedIntegrationConfigKeys` — that list is currently limited to credentials granting access to an external secret backend (Vault, Infisical, S3).

**Why it matters:** the current snapshot answers "is this host alive", not "what is it actually doing". Note what Glances does *not* give you: it is real-time and keeps no history of its own, so it explains a problem happening now, not one that happened during last night's deploy. That makes it complementary to — not a substitute for — the per-stack Prometheus metrics entry in Tier 1: Prometheus is wireops' own deploy state over time, Glances is host and container resources right now. Anyone wanting retrospective host data points Glances' own exporters at Prometheus, which is their setup to run, not wireops'.

### Backup verification (restore drill)

**Today:** `internal/backup/` produces backups, mirrors them off-host to S3, and can restore — the restore path is covered by tests (`backup_restore_test.go`). Nothing verifies that a *real, stored* backup restores.

**Missing:** a scheduled drill that pulls the most recent artifact, restores it into a throwaway `DATA_DIR`, runs `VerifyOrSeedSecretKeyCanary` plus a collection/row sanity check against it, and fires a notification on failure. Reuses restore and the canary as-is; the new part is the temporary-instance sandbox and the schedule.

**Why it matters:** today the first real test of a backup is the disaster. This pairs with the existing `backup.mirror_error` event — mirroring proves the file arrived, a drill proves the file is worth having.

### Lint as a pull-request check

**Today:** `internal/lint/` is server-side and daemon-free, and its error-severity findings already gate deploys and stack creation — warning- and info-severity findings are advisory only. `internal/gitprovider` already holds a working GitHub OAuth connection.

**Missing:** publishing those findings as a commit status / check run when a push arrives, with per-line annotations (the lint already reports line numbers — the Review step renders them today). The check would mirror the existing gate: only error-severity findings fail the check/block merge via branch protection; warning- and info-severity findings post as advisory annotations, same as they are advisory in the existing create/deploy gate.

**Why it matters:** the gate currently fires at deploy time, which is *after* the merge. The same engine posting on the pull request moves the failure to where it can still be fixed cheaply, and costs no new analysis logic — only a new output surface on top of `internal/gitprovider`.

### Synchronous deploy for CI gating

**Today:** `Scheduler.TriggerSync` (`internal/sync/scheduler.go`) is fire-and-forget, and the webhook endpoint answers immediately. The full result — dispatch, deploy, and post-check landing on `active` / `degraded` / `error` — is resolved asynchronously by `internal/sync/postcheck.go`.

**Missing:** an endpoint that triggers a sync and blocks until that state machine settles or a caller-supplied timeout expires, returning the final status and the sync log id. Optionally a thin GitHub Action wrapping it.

**Why it matters:** a pipeline cannot currently fail when the deploy fails — CI reports success as soon as wireops accepts the webhook. Every state this needs already exists and is already persisted; what is missing is a way to wait for it.

### Server-side pagination, filtering, and sorting on list views

**Today:** partially done. Stacks (`StacksPanel.vue`), Jobs (`JobsPanel.vue`), and the Identity users list (`settings/identity/index.vue`) now page/filter/sort server-side via a shared `usePaginatedList` composable (`frontend/app/composables/usePaginatedList.ts`) instead of `getFullList()` + client-side filtering. The Jobs list endpoint (`GET /api/custom/jobs`) also moved its DB filter (status/repository/search) ahead of the expensive per-job `job.yaml` parse and repo/run lookups, so those only run for the page actually returned.

Two deliberate gaps in the conversion, not oversights:
- **Stack search no longer matches container names.** That match was against `containers_list`, a JSON field with no server-side equivalent short of a search index; keeping it would have meant falling back to an unpaginated fetch, defeating the point.
- **Jobs can't filter or sort by `group` server-side**, because group only exists inside each job's parsed `job.yaml`, not a stored column. It's applied client-side over whatever page is loaded, which can under-report matches sitting on a different page. Promoting `group` to a synced column (written back to the DB on every successful parse) would close this; out of scope here.

**Missing:** the remaining ~28 `getFullList()` call sites, roughly: repositories (`pages/repositories/index.vue`), secrets (`pages/secrets/index.vue`), audit-adjacent lists still on `getFullList`, integrations config lists, and the command palette's search index (`AppCommandPalette.vue`, which intentionally still loads everything since it's an instant local search-as-you-type index — converting it would need a server-side search endpoint, not just pagination). Each can follow the same `usePaginatedList` pattern the three converted lists now demonstrate.

**Why it matters:** the current design is fine at twenty stacks and gets steadily worse from there — every page load transfers the whole fleet and every keystroke filters it client-side. It is also a precondition for the environments and scoped-RBAC entries in Tier 3, both of which assume the server decides which records a list contains.

### In-app notification centre

**Today:** notification events reach Discord, Slack, ntfy, or a webhook (`internal/notify/`). Inside the panel, the same events are only visible as the ten most recent syncs on the dashboard.

**Missing:** a notification surface in the app — unread since last visit, filterable, backed by the same event constants — so the panel is a delivery target alongside the existing plugins.

**Why it matters:** an operator who hasn't configured Slack has no passive channel at all; failures are found by looking. This is the third leg of the notification work: Tier 1 adds the missing events, the entry below routes and throttles them, and this one gives them somewhere to land for people who don't want a chat integration.

### Notification routing and throttling

**Today:** notification events are configured per delivery plugin, globally (`internal/notify/`).

**Missing:** routing by stack or worker tag, deduplication/throttling per event, and quiet hours.

**Why it matters:** this should ship *with* the new event types proposed in Tier 1, not after them. Adding `job.failed`, `worker.offline`, `policy.violation`, and `lint.error` to a global fan-out turns a flapping worker into a channel nobody reads — which costs more than the missing events did.

### Drift detection and self-healing

**Today:** the reconcile loop compares the repository's latest commit SHA against the stored one. If someone runs `docker compose down` by hand, or edits a container on the host, wireops notices nothing until the next commit lands.

**Missing:** a periodic sweep that compares live container state (the `GetStatusCommand` used by the post-check already returns it) against the deployed revision, marks the stack as drifted, and optionally redeploys to correct it.

**Why it matters:** this is the widest conceptual gap between what wireops does today and what "GitOps controller" implies — reconciling *actual* state, not just tracking commits.

### Deploy windows and manual approval

Per-stack rules like "never deploy between Friday 18:00 and Monday 08:00" or "production requires an admin to approve". The waiting mechanism already exists: `stack_pending_reconciles` queues reconciles for later replay when a worker is offline, and RBAC already distinguishes operator from admin.

### Multiple compose files and profiles

`compose_path` currently accepts a single file. Supporting `-f base.yml -f override.prod.yml` and Compose profiles — declared in `wireops.yaml` — matches how people actually version compose files across several hosts.

### Scheduled job improvements

Retry with backoff, "skip this tick if the previous run is still going", per-job failure notifications (see the Tier 1 event work), and dependencies between jobs.

### Registry watcher — image updates the GitOps way

For services pinned to a floating tag, poll the registry for a new digest and then **open a pull request against the repository** via the existing `internal/gitprovider` package (GitHub OAuth is already wired), instead of updating containers behind Git's back the way Watchtower does.

This pairs naturally with the existing lint rule that flags unpinned tags: the lint says "this isn't pinned", and this feature is what makes pinning practical to live with. The most compelling feature on this page, and also the most expensive in Tier 2.

### Bulk operations

Multi-select stacks with "Sync All" / "Pause All", and progress tracking for the batch.

### Stack health history

Workers already store a `health_history` field rendered as a trend in the UI. Stacks don't. The pattern to copy already exists.

---

## Tier 3 — strategic

### Scoped RBAC — grants over a subset of the fleet

**Today:** `internal/rbac/rbac.go` maps each capability to a minimum role, evaluated globally. `viewer` means every stack, `operator` means deploy anything on any host, and there is no notion of a user owning or being restricted to part of the fleet.

**Missing:** grants scoped by worker tag or environment — "operator on `staging`, viewer on `prod`". The capability set stays as-is; what changes is that the check needs the resource, not just the user, and every list endpoint has to filter rather than answer wholesale.

**Why it matters:** this is the ceiling on multi-team use. It is Tier 3 not because the model is hard but because the blast radius is: every custom route, every PocketBase collection rule, and the MCP server's read surface all currently assume a global answer. Sequencing note — this should follow the environments entry below, since an environment is the obvious thing to scope a grant *to*.

### Instance configuration as code

**Today:** [Disaster recovery](DISASTER_RECOVERY.md) covers restoring the authoritative PocketBase data, and `internal/backup/` mirrors it off-host. There is no way to declare an instance — repositories, stacks, workers, policies, integrations, service accounts — as a reviewable file.

**Missing:** an export that renders the current instance into a manifest, and an apply that reconciles an instance toward one, with secret *values* excluded by reference (they already live in Vault/Infisical/SOPS for anyone who configured a provider).

**Why it matters:** worker security policy is one of the most privileged settings in the system and today it changes through a form, with an audit log entry as the only record — no review, no diff, no revert. The project's entire argument for compose files applies to its own configuration. It also turns "rebuild the instance elsewhere" into something other than restoring a SQLite file onto a host that must already hold the matching `SECRET_KEY`.

### Environments as an inheritance layer

**Today:** stack-specific variables live in `stack_env_vars`, while reusable values live in `global_env_vars` and are attached through binding records. Policies are per-worker, notification config is global, and nothing groups stacks into dev/staging/prod as a first-class unit.

**Missing:** an `environments` collection holding shared variables and secrets, a default policy, worker tags, and notification targets, with precedence resolved as stack over environment at render time (`internal/sync/renderer.go`, `internal/envvars/`).

**Why it matters:** global variables cover shared values, but they do not bundle policy, worker placement, and notification behavior into an environment. The shipped copy-between-stacks feature is useful for cloning configuration, but copied values can still diverge. An environment layer is also the natural prerequisite for preview environments below.

### Agent proposal queue — the write tier for MCP

**Today:** the MCP server is read-only by construction: it holds no credential of its own, passes through the client's `viewer` API key, and RBAC refuses anything above that role. The [AI discovery notes](https://github.com/wireops/wireops/wiki/AI-Discovery) deliberately left the question of an `operator`-scoped tier open, with the recommendation that it arrive as dry-run plus explicit human confirmation rather than as a wider key.

**Missing:** a `pending_actions` collection, an MCP `propose_action` tool (deploy, rollback, env change) that only ever writes to it, an approval screen, and execution through the existing dispatch path once a human approves. `stack_pending_reconciles` is already exactly this shape — a queued intent replayed later — so the storage and replay pattern exists.

**Why it matters:** it is the only design on the table that lets an agent act without any agent ever holding `operator`. The audit log gains a proposer/approver pair rather than a single actor, which is strictly more information than a human doing the same operation by hand produces today. Tier 3 because the cost is in the approval UX and the semantics of a stale proposal (the commit moved, the worker went away), not in the plumbing.

### Internationalization

**Today:** there is no i18n infrastructure — no `@nuxtjs/i18n`, no message catalogue, and user-facing strings are written inline across ~99 components, plus error text and suggestions produced server-side and in `utils/error-parser.ts`.

**Missing:** a message catalogue, extraction of existing strings, locale detection and switching, locale-aware dates and numbers, and a translation contribution workflow.

**Why it matters:** self-hosters are not concentrated in one language, and the audience the project targets — homelabs and small teams — is exactly where an English-only panel is the barrier. Tier 3 purely on size: the change is mechanical but touches nearly every component, and the ongoing cost is keeping translations from rotting. Worth deciding early only in one respect — new components written string-inline today are the ones that will need rewriting later.

### Preview environments per branch or pull request

Ephemeral stacks created for a branch and torn down on merge. Fits well with the Git provider OAuth integration, but it's a large piece of work touching sync, naming, routing, and cleanup.

### Ordering and dependencies between stacks

Deploy stack A only after stack B is healthy. Conceptually clean, but it introduces cross-stack scheduling state that nothing in the system models today.

### OCI artifact sources, Swarm / multi-node, canary deploys

Long-standing backlog entries with no ETA. All expensive, and canary deploys in particular pull against the project's stated scope — wireops deliberately isn't trying to be a Kubernetes-grade orchestrator.

---

## Contributing a proposal

Open an issue describing the use case first, rather than starting with a pull request. The project is actively developed at a hobby/side-project pace, so agreeing on scope before implementation keeps review practical and avoids work that conflicts with the product boundary.
