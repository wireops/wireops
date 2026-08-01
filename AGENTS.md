# wireops — Agent Context

Self-hosted GitOps platform for managing Docker Compose stacks and scheduled Docker-based jobs. It watches Git repositories for changes and deploys updates on remote hosts through token-authenticated WebSocket workers.

**Scope**: targets developers, homelabs, and self-hosters running plain `docker compose` stacks across one or many hosts/repos — without manifest sprawl or enterprise-grade autoscaling/control-plane complexity. **Not** a Kubernetes/Swarm alternative, not a competitor to Flux/ArgoCD/enterprise app-management platforms; cluster orchestration, multi-node scheduling, and autoscaling are explicitly out of scope. For production/internet-exposed workloads at enterprise scale, point users toward Kubernetes (Flux/ArgoCD) instead of stretching wireops into that role.

**Project status**: pre-1.0 (tags `v0.1.0`–`v0.1.15`), active hobby/side-project pace. Core GitOps sync, worker deploy security policy, and RBAC/audit are implemented and in daily use. `internal/secrets` has all three providers implemented and functional: `internal` (AES-GCM), `vault` (HashiCorp Vault), and `infisical`. `internal/backup` (create/list/upload/delete/restore, cron autobackup) is implemented and shipped; optional off-host mirroring to S3-compatible storage is the "s3" `internal/integrations` entry (see `internal/backup/remote`). Don't describe stub features as shipped in docs or responses without checking the source first.

---

## Table of Contents

- [Repository Layout](#repository-layout)
- [Tech Stack](#tech-stack)
- [Architecture](#architecture)
- [Data Model](#data-model)
- [Key Business Flows](#key-business-flows)
- [Custom API Endpoints](#custom-api-endpoints)
- [Environment Variables](#environment-variables)
- [Integration Plugin System](#integration-plugin-system)
- [Notifications](#notifications)
- [Access Control, Policy & Audit](#access-control-policy--audit)
- [Coding Conventions](#coding-conventions)
- [Testing & Coverage](#testing--coverage)
- [Git Workflow](#git-workflow)

---

## Repository Layout

```
.
├── main.go                       # Entrypoint — delegates to cmd/serve.go
├── cmd/serve.go                  # Server bootstrap & dependency wiring
├── go.mod                        # Go module (go 1.25)
├── worker/                       # Standalone remote-worker binary
│   ├── main.go
│   ├── api/client.go             # Token-authenticated HTTP client (register)
│   ├── executor/runner.go        # Executes deploy / teardown / job commands
│   └── sync/websocket.go         # Persistent WebSocket connection to server
├── internal/
│   ├── worker/                   # Server-side worker management
│   │   ├── server.go             # Token-authenticated WebSocket server on :8443
│   │   └── service.go            # Worker CRUD, registration tokens, health tracking
│   ├── compose/                  # Docker Compose helpers
│   │   ├── config.go             # Compose YAML parsing
│   │   ├── runner.go             # RunUp / RunDown / RunForceUp / RunPs
│   │   └── status.go             # Container status, stats, volumes, networks
│   ├── config/config.go          # APP_URL, webhook URL resolution
│   ├── crypto/encrypt.go         # AES-GCM encryption for secrets at rest
│   ├── docker/client.go          # Docker Engine API client wrapper
│   ├── git/                      # Clone, fetch, SSH/Basic auth
│   ├── hooks/pb_hooks.go         # PocketBase lifecycle hooks
│   ├── integrations/             # Plugin registry (Traefik, Caddy, Nginx Proxy Manager, Dozzle, Webhook, Discord, Slack, Ntfy, SOPS)
│   ├── job/parser.go             # job.yaml parsing & validation
│   ├── jobscheduler/scheduler.go # Cron scheduler for Docker-based jobs
│   ├── lint/                     # Static compose checks (rule registry + worker-policy adapter); advisory, server-side, no Docker daemon
│   ├── manifest/parser.go        # Parses declarative `.wireops.yml` stack config
│   ├── notify/                   # Outbound notifications (webhook / ntfy) — superseded by integrations/ notification plugins for new work
│   ├── policy/                   # Worker-level deploy security policies (block privileged/host-network/docker.sock/host-PID/host-IPC, allowlists)
│   ├── protocol/messages.go      # WebSocket message types (shared)
│   ├── rbac/rbac.go              # Role definitions (viewer/operator/admin/monitoring) and capability checks
│   ├── audit/audit.go            # Request/system audit log recording + retention purge
│   ├── secrets/                  # Pluggable secret providers: internal (AES-GCM), vault (HashiCorp Vault), infisical; also SOPS+age helpers (keypair gen, decrypt/encrypt secrets.yaml) used by sync/ and hooks/
│   ├── oidc/collection.go        # OIDC PocketBase collection support (client secret hydration)
│   ├── setup/service.go          # First-admin bootstrap (`/setup`) service
│   ├── backup/                   # Backup/restore (create/list/upload/delete/restore) + optional S3 mirroring
│   ├── routes/                   # HTTP route handlers
│   │   ├── routes.go             # Stack / repo / credential / integration routes
│   │   ├── worker.go             # Worker management routes
│   │   ├── jobs.go               # Scheduled job routes
│   │   └── users.go              # User management
│   ├── safepath/                 # Path traversal protection
│   └── sync/
│       ├── scheduler.go          # Per-stack polling scheduler
│       ├── reconciler.go         # Core GitOps reconcile loop
│       ├── renderer.go           # Injects wireops labels into compose YAML
│       └── watcher.go            # File-based change detection
├── pb_migrations/                # PocketBase SQLite schema migrations
├── pb_public/                    # Compiled frontend static assets (served by PocketBase)
├── mcp/                          # Standalone MCP server binary (wireops-mcp) — read-only tools + generate/scaffold tools, pass-through auth
└── frontend/                     # Nuxt 4 SPA (Vue 3, @nuxt/ui v4, Tailwind)
    └── app/
        ├── pages/                # File-based routing
        ├── components/           # Reusable Vue components
        ├── composables/          # useApi, useAuth, useRealtime, …
        ├── layouts/default.vue
        └── plugins/pocketbase.ts # PocketBase JS SDK setup
```

---

## Tech Stack

| Layer | Technology |
|---|---|
| Backend language | Go 1.25 |
| Backend framework | PocketBase v0.36 (embedded SQLite, REST, realtime SSE) |
| HTTP routing | PocketBase router + Gin (worker server only) |
| Database | SQLite via PocketBase |
| Git operations | `go-git/go-git/v5` |
| Docker client | `docker/docker` (Engine API v28) |
| WebSocket | `gorilla/websocket` |
| Encryption | AES-GCM via `golang.org/x/crypto` |
| Scheduler | `robfig/cron/v3` |
| Frontend | Nuxt 4 (Vue 3), SSR disabled — static SPA |
| UI library | `@nuxt/ui` v4 (Tailwind + Headless UI) |
| Frontend–backend comms | PocketBase JS SDK + custom REST calls |

---

## Architecture

Two deployable components:

```
┌──────────────────────────────────────────────────┐
│                wireops Server                    │
│  ┌────────────┐  ┌──────────────┐  ┌──────────┐ │
│  │ PocketBase │  │ Sync         │  │ Job      │ │
│  │ API :8090  │  │ Scheduler    │  │ Scheduler│ │
│  └────────────┘  └──────────────┘  └──────────┘ │
│  ┌────────────────────────────────────────────┐  │
│  │  Worker WebSocket Server :8443              │  │
│  └────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────┘
              ↑ WebSocket + Token ↑
  ┌──────────────────────────────┐
  │   Remote Worker              │
  │   Executes docker compose    │
  │   and docker run (jobs)      │
  └──────────────────────────────┘

  Nuxt SPA (pb_public/) served by PocketBase
  ← REST + PocketBase Realtime (SSE)
```

- The server **never** runs `docker compose` or `docker run` directly — all stack deployments and job executions are dispatched over a persistent WebSocket to connected remote workers.
- All deploy and job execution happens through connected remote workers.
- PocketBase handles auth (superusers collection), realtime subscriptions, and the SQLite database.
- The frontend is statically generated (`nuxt generate`) and served from `pb_public/`.

---

## Data Model

All collections are defined via Go migrations in `pb_migrations/`.

| Collection | Key Fields |
|---|---|
| `repositories` | `name`, `git_url`, `branch`, `status`, `last_commit_sha`, `platform`, `sops_age_key`* (auto-generated), `sops_age_public_key` |
| `repository_keys` | `repository`, `auth_type` (none/ssh_key/basic), `ssh_private_key`*, `git_password`* |
| `stacks` | `name`, `repository`, `compose_path`, `auto_sync`, `status`, `worker`, `current_version`, `render_overrides` (JSON, per-service image/ports/networks not committed to git) |
| `stack_env_vars` | `stack`, `key`, `value`*, `secret`, `secret_provider` (internal/vault/infisical) |
| `global_env_vars` | `key`, `value`*, `secret`, `secret_provider` — reusable across stacks/jobs via binding tables |
| `job_env_vars` | `job`, `key`, `value`*, `secret`, `secret_provider` |
| `stack_global_env_vars` / `job_global_env_vars` | Binding tables linking `global_env_vars` rows to stacks/jobs |
| `stack_services` | `stack`, `service_name`, `container_name`, `status` |
| `stack_revisions` | `stack`, `version` — numbered snapshots of rendered compose YAML |
| `stack_pending_reconciles` | `stack`, `trigger`, `commit_sha` — queue for offline worker reconnect |
| `sync_logs` | `stack`, `trigger`, `status`, `output`, `duration_ms` |
| `workers` | `hostname`, `fingerprint`, `status` (ACTIVE/REVOKED), `health_history` |
| `scheduled_jobs` | `repository`, `job_file`, `enabled`, `status` |
| `job_runs` | `job`, `worker`, `status`, `output`, `expires_at` (30-day TTL) |
| `integrations` | `slug`, `enabled`, `config` (JSON) — also stores Vault/Infisical backend config (address/token, site_url/client_id/client_secret, etc.) |

(\* = AES-GCM encrypted at rest via `SECRET_KEY`)

**Relationships:**
```
repositories ─── 1:N ──→ stacks ─── 1:N ──→ stack_env_vars
                                 └── 1:N ──→ sync_logs
                                 └── 1:N ──→ stack_revisions
                                 └── N:1 ──→ workers
repositories ─── 1:N ──→ scheduled_jobs ─── 1:N ──→ job_runs ─── N:1 ──→ workers
```

---

## Key Business Flows

### GitOps Sync
1. `sync/scheduler.go` polls each stack on an interval set by `SCAN_PERIOD` (default 10s, `internal/config.GetScanPeriod`); a stack's own positive `sync_interval_seconds` (from wireops.yaml's `sync.interval`) overrides this fallback for that stack.
2. `sync/reconciler.go` runs `git.CloneOrFetch` and compares the latest commit SHA with the stored one.
3. `sync/lint.go` runs `internal/lint` over the resolved compose config and records a `lint` phase on the deploy timeline. This run is advisory — it never aborts the reconcile itself, it just gives the timeline a lint summary early. The renderer (step 4) runs its own `lint.Run` after applying render overrides and *does* abort the deploy on any error-severity finding, alongside the worker-policy check it already ran.
4. `sync/renderer.go` reads the compose YAML, injects `dev.wireops.*` labels, and writes a versioned revision file to `DATA_DIR/stacks/<id>/v<n>.yml`.
5. The server base64-encodes the compose file, sends a `DeployCommand` over WebSocket to the worker assigned to the stack, and waits up to 5 minutes for `CommandResult`.
6. The worker decodes the compose file and executes `docker compose up`.
7. Persists a `sync_logs` entry, updates stack status, fires webhook/ntfy notification.

### Worker Bootstrap & Communication
1. Admin generates a token via the UI/API.
2. Worker connects to `POST /worker/register` (using the HTTPS endpoint on port :8443) with the `X-Wireops-Worker-Token` header to register.
3. The server validates the token (transitioning it from `STAGING` to `ACTIVE`) and associates it with the worker.
4. Worker opens a persistent WebSocket connection to `/worker/ws` on the worker server, authenticated via the same token.
5. Server dispatches typed commands (`DeployCommand`, `TeardownCommand`, `RunJobCommand`, etc.) as JSON `Envelope` messages.
6. Worker sends heartbeats every 30s; job completions are pushed as unsolicited `MsgJobCompleted` messages.
7. If a worker is offline when a change is detected, a `stack_pending_reconciles` record is created and replayed on reconnect.

### Scheduled Jobs
1. A `scheduled_jobs` record points to a `job.yaml` file inside a repository.
2. `job.yaml` specifies: `title`, `cron`, `image`, `command`, `tags`, `mode` (once / once_all), `volumes`, `network`, and a mandatory `resources` block containing `cpu`, `memory`, and `timeout`.
3. `jobscheduler/scheduler.go` registers a cron entry for each enabled job.
4. On tick: resolves matching workers by tags, creates a `job_runs` record, dispatches `RunJobCommand` via WebSocket.
5. Worker runs `docker run --rm` asynchronously, then pushes `MsgJobCompleted` with exit code and output.
6. Server marks the run as `success` or `error`, updates `last_run_at`. Stalled runs (> 1 hour) are swept every 5 minutes.

---

## Custom API Endpoints

All custom routes are prefixed `/api/custom/`. PocketBase also auto-exposes CRUD REST for all collections.

### Stacks
| Method | Path | Description |
|---|---|---|
| `POST` | `/stacks/{id}/sync` | Trigger git sync |
| `POST` | `/stacks/{id}/rollback` | Rollback to a commit SHA |
| `POST` | `/stacks/{id}/force-redeploy` | Force recreate containers/volumes/networks |
| `POST` | `/stacks/{id}/transfer` | Move stack to another worker |
| `DELETE` | `/stacks/{id}` | Teardown & delete stack |
| `GET` | `/stacks/{id}/services` | Live container statuses |
| `GET` | `/stacks/{id}/resources` | Volumes + networks |
| `GET` | `/stacks/{id}/compose` | Read rendered compose YAML |
| `GET` | `/stacks/{id}/revisions/{version}` | Read rendered compose YAML for a specific version |
| `GET` | `/stacks/{id}/render-overrides` | View persisted render-time overrides (+ diff vs git) |
| `PUT` | `/stacks/{id}/render-overrides` | Set per-service image/ports/networks overrides (not committed to git); gated by `allow_render_overrides` worker policy; force-recreates |
| `DELETE` | `/stacks/{id}/render-overrides` | Clear render overrides; force-recreates |
| `GET` | `/stacks/{id}/stream` | SSE log stream |
| `GET` | `/stacks/{id}/container/{cid}/stats` | CPU/mem stats |
| `GET` | `/stacks/{id}/container/{cid}/logs` | Container logs |
| `POST` | `/stacks/{id}/container/stop` | Stop a container |
| `POST` | `/stacks/{id}/container/restart` | Restart a container |
| `GET` | `/stacks/import/discover` | Discover unmanaged Compose projects |
| `POST` | `/stacks/import` | Import a local Compose stack |

### Lint

| Method | Path | Description |
|---|---|---|
| `POST` | `/lint/compose` | Static checks on a repo's compose file (`repository`, `compose_path`, `compose_file`, optional `worker` for policy findings and `stack` for env-var resolution). Needs no stack to exist and no worker to be online — used by the create-stack modal's Review step. Returns `{report, content?, filename?, config_error?}` — `content` is the source file, so the UI can preview it and mark each finding's line; a compose file that won't parse comes back as `config_error` with HTTP 200, not a request failure. Requires `CapManageRepos`; see the lint bullet under Access Control for the constraints this route has to keep. |

### Repositories
| Method | Path | Description |
|---|---|---|
| `GET` | `/repositories/{id}/commits` | Last 5 commits |
| `GET` | `/repositories/{id}/files` | List `.yml`/`.yaml` files |
| `POST` | `/credentials/test` | Test git credentials |
| `POST` | `/credentials/keyscan` | SSH host key scan |

### SOPS (`internal/routes/sops_routes.go`)

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/custom/stacks/{id}/sops-env-vars` | List `secrets.yaml` key names only, never values (`CapViewStacks`) |
| `POST` | `/api/custom/repositories/{id}/sops-rotate-key` | Regenerate the repo's age keypair — old `secrets.yaml` becomes undecryptable until re-encrypted (`CapManageRepos`) |
| `POST` | `/api/custom/repositories/{id}/sops-encrypt` | Encrypt a key/value map into `secrets.yaml` content using the repo's public key; nothing persisted server-side (`CapManageRepos`) |

### Workers (superuser only)
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/custom/workers` | List all workers (including pending tokens) |
| `POST` | `/api/custom/worker/tokens` | Generate worker token |
| `POST` | `/api/custom/workers/{id}/revoke` | Revoke worker or a pending token (using `pending:{tokenRecordId}`) |

### Worker Policy (`CapManageSettings`, not superuser-only)
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/custom/workers/{id}/policy` | Resolved effective deploy security policy for a worker, plus its local overrides |
| `PUT` | `/api/custom/workers/{id}/policy` | Set per-worker policy overrides |
| `DELETE` | `/api/custom/workers/{id}/policy` | Clear per-worker overrides (revert to inherit) |
| `GET` | `/api/custom/settings/worker-policy` | Global `worker_policies` singleton |
| `PUT` | `/api/custom/settings/worker-policy` | Update global `worker_policies` singleton |

### Audit (`admin` capability — `CapViewAuditLogs`)

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/custom/audit-logs` | Filterable audit log query (from/to/actor/action/resource/origin/status) |

### Users

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/custom/users/invite` | Invite a new user (`CapManageUsers`) |

### Metrics (`monitoring` role or higher; API key on service account)
| Method | Path | Description |
|---|---|---|
| `GET` | `/metrics` | Aggregated Prometheus metrics (canonical; same port as UI) |
| `GET` | `/api/custom/metrics` | Alias of `/metrics` |
| `GET` | `/api/custom/workers/{id}/metrics` | Metrics from a single connected worker |

### Scheduled Jobs
| Method | Path | Description |
|---|---|---|
| `GET` | `/jobs` | List jobs with definitions |
| `POST` | `/jobs/{id}/run` | Trigger manual run |
| `POST` | `/job-runs/{runId}/cancel` | Kill running container |
| `DELETE` | `/job-runs/{runId}` | Delete stalled run |

### Secret Backend Browse (superuser only, read-only — never returns raw credentials)

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/custom/integrations/vault/mounts` | List KV v2 mounts |
| `GET` | `/api/custom/integrations/vault/browse` | Browse paths/keys under a mount |
| `GET` | `/api/custom/integrations/vault/fields` | List fields at a path |
| `POST` | `/api/custom/integrations/vault/test` | Test Vault connection |
| `GET` | `/api/custom/integrations/infisical/projects` | List projects |
| `GET` | `/api/custom/integrations/infisical/project` | Project detail (environments) |
| `GET` | `/api/custom/integrations/infisical/browse` | Browse secret paths/keys |
| `POST` | `/api/custom/integrations/infisical/test` | Test Infisical connection |

---

## Environment Variables

### Server
| Variable | Description |
|---|---|
| `SECRET_KEY` | **Required.** 32-byte AES key for encrypting secrets at rest |
| `APP_URL` | Base URL for CORS, webhooks, and emails (default: `http://localhost:8090`) |
| `PORT` | UI, REST API, and Prometheus `/metrics` (default: `8090`) |
| `TLS_WORKER_PORT` | Worker WebSocket/register TLS port (default: `8443`) — not for Prometheus |
| `DATA_DIR` | Root runtime data directory (default: `./data`) |
| `PB_DATA_DIR` | Optional override for PocketBase SQLite data directory (default: `DATA_DIR/pb_data`) |
| `REPOS_WORKSPACE` | Optional override for Git clone workspace (default: `DATA_DIR/repos`) |
| `COMPOSE_MAX_KB` | Max size of a resolved `docker compose config` the server will buffer and parse (default: `512` KB). Bounds memory driven by repository content; raise only for a genuinely huge stack |

### Worker
| Variable | Description |
|---|---|
| `SERVER_URL` | **Required.** HTTPS URL of the wireops server |
| `WORKER_TOKEN` | **Required.** Worker authorization token |
| `WORKER_TAGS` | **Required.** Comma-separated tags (used for job routing). No spaces or special characters — letters, numbers, `-` and `_` only |

---

## Integration Plugin System

Integrations live in `internal/integrations/` and implement:

```go
type Integration interface {
    Slug() string
    Name() string
    Category() string
    ResolveContainerActions(container ContainerInfo, config map[string]any) []ContainerAction
}
```

Registered via `init()` and `integrations.Register()`. Current plugins:
- **Traefik** (`traefik`) — Reverse Proxy — reads router labels and builds clickable "Open" links.
- **Caddy** (`caddy`) — Reverse Proxy — same pattern as Traefik for Caddy labels.
- **Nginx Proxy Manager** (`nginxproxymanager`) — Reverse Proxy.
- **Dozzle** (`dozzle`) — Logging — adds "Logs" links pointing to a self-hosted Dozzle instance.
- **Webhook** (`webhook`) — Notification — HMAC-SHA256 signed HTTP POST.
- **Discord** (`discord`) — Notification.
- **Slack** (`slack`) — Notification.
- **Ntfy** (`ntfy`) — Notification — push via ntfy.sh.

---

## Notifications

Historically configured via the `stack_sync_events` singleton collection with two built-in providers (Webhook, Ntfy). Discord and Slack were added as `internal/integrations/` notification plugins rather than extending `stack_sync_events` directly — treat `integrations/` as the current extension point for new notification channels; `internal/notify/` remains for the original webhook/ntfy wiring.

Events: `sync.started`, `sync.done`, `sync.error`, `sync.test`.

---

## Access Control, Policy & Audit

- **RBAC** (`internal/rbac/rbac.go`): four roles — `viewer` < `operator` < `admin`, plus a separate `monitoring` role scoped to metrics-only access. Capabilities (`CapViewStacks`, `CapOperateStacks`, `CapManageSettings`, `CapManageSecurity`, `CapViewAuditLogs`, …) map to a minimum required role; route handlers check capabilities, not raw role strings.
- **Worker deploy policy** (`internal/policy/`): resolves an effective `WorkerPolicy` per worker from the `worker_policies` singleton (global) with optional per-worker overrides. Enforces allowlists (volumes/networks/images/cap-add/devices/security-opt) and boolean blocks (`BlockPrivileged`, `BlockHostNetwork`, `BlockHostPID`, `BlockHostIPC`, `BlockDockerSocket`, `PreventLatestImages`, `BlockHostVolumes`), plus `AllowRenderOverrides` (off by default) gating whether a stack's `render_overrides` (per-service image/ports/networks, not committed to git — see `internal/sync/renderer.go`'s `ApplyServiceOverrides`) may be applied at all; overridden values still pass through the same allowlist/block checks after being applied. Empty allowlist = open; first entry added = allowlist-only from then on. `policy_inherit` controls whether a worker without a local override falls back to the global value. Two entry points: `ValidateComposeConfig` returns the first violation and is what blocks a deploy (renderer, render-overrides route); `ComposeViolations` returns all of them, structured per service, and is what `internal/lint` reports. Both run off the same extraction and produce identical messages — keep it that way when adding a check.
- **Path containment** (`internal/compose.openContained`): `safepath`'s `Validate*` helpers only inspect the path *string* — they reject `..`, absolute paths and bad extensions, which is everything a request parameter can express, but they cannot see what the filesystem does with the result. Git preserves symlinks on checkout (mode 120000), so a repository shipping `docker-compose.yml -> /etc/passwd` passes every string check: the extension check constrains the *link's name*, never its target. Reads whose path is influenced by repository content go through **`os.Root`** (Go 1.24+) rather than validating a path and re-opening it: `os.Root` performs the traversal itself, refuses symlinks that escape, and — because the caller keeps the descriptor — lets the size check and the read act on the same open file instead of re-consulting the path. That last part matters: the repo checkout is shared between concurrent requests and rewritten by every git fetch, so check-then-open is a genuine race. `compose.ResolveFile`/`ReadFile` use it, rooted at `ConfigOptions.Root`, which defaults to `WorkDir` so a forgotten root still cannot escape its own directory. `Config` **pipes the validated bytes to docker over stdin (`-f -`)** rather than naming the file: naming it would make docker resolve the path a second time, and a concurrent fetch into the shared checkout could swap it for a symlink in between. Compose resolves relative paths in a stdin document against the working directory — the same directory the file lives in — so bind sources, `env_file` and the derived project name are byte-identical to naming the file (verified against the CLI). This covers the compose file itself; `env_file`/`include` targets it references are still opened by docker from `WorkDir`.
- **Compose config size limit** (`internal/compose/limitedbuffer.go`): every `compose.Config` call — renderer, lint, both render-override routes — buffers the CLI's stdout through a bounded writer capped at `COMPOSE_MAX_KB` (default 512 KB), with stderr separately capped at 64 KB since it only feeds error messages. The size is attacker-influenced (it comes from a repo's compose file), and the buffer is then JSON-decoded and walked, so it needs a ceiling. It **errors rather than truncates**: a truncated config would still parse, as a smaller stack missing services or the very fields the policy checks — always a hard failure, never a silent one. Callers can distinguish it with `errors.Is(err, compose.ErrOutputTooLarge)`.
- **Compose lint** (`internal/lint/`): static analysis of a resolved compose config — structural checks (no services, missing image, undeclared network/volume, undefined `${VAR}`) plus advisory ones (unpinned tag, no restart policy/healthcheck/resource limit, port on all interfaces, plaintext secret, relative bind mount, privileged/host-namespace/docker-socket), with worker-policy breaches folded in at error severity via the adapter in `rules_policy.go`. Rules register themselves from `init()` like `internal/integrations`; `Run` sorts by severity then rule then service so output is deterministic despite Go's map iteration. Each `Finding` also carries a `Title` — a short label for card-style UIs — filled in by `Run` from a `ruleTitles` map keyed by the same `RuleXxx` constants the rules register with, falling back to a humanized rule ID for anything unmapped. The `lint.Run` call itself makes no enforcement decision (it always returns every finding regardless of severity), but two callers gate on the result: the renderer (`internal/sync/renderer.go`'s `GenerateRevision`) refuses to produce a revision, and the stacks `OnRecordCreate` hook (`internal/hooks/pb_hooks.go`'s `rejectStackOnLintErrors`) refuses to create the record, when the report has any error-severity finding — both run the check *after* render-time overrides/policy would already apply, so an override that fixes a flagged violation is not rejected for having been flagged. Other callers, like the compose lint preview route, surface the same report purely as advice. Runs entirely server-side off `docker compose config` (a client-side parse), so it needs no Docker daemon and no online worker; daemon-dependent checks (image exists, host port free) would belong on a worker and are deliberately absent. Callers: the `lint` deploy-timeline phase (`internal/sync/lint.go`, advisory-only — see the sync-loop step above), `POST /api/custom/lint/compose`, the renderer, and the stacks create hook. Note `Context.EnvKeys`: wireops renders with `--no-interpolate`, so a surviving `${VAR}` is normal — the undefined-variable check stays off unless the caller supplies the env-var set.
  - **Source line mapping** (`internal/lint/source.go`): rules run against the resolved config, which has no position information and is not line-for-line the file on disk, so `ResolveLines` parses the *source* separately into a `yaml.Node` tree (which carries `Line`) and walks each finding's `Path` against it to fill in `Finding.Line`. Two things it must keep doing: matching keys longest-first rather than splitting the path on `.` (a compose service may legitimately be named `my.api`), and resolving an unmatched path to its **deepest existing prefix** — that is the normal case, not a fallback, since a "service X sets no memory limit" finding has a path (`services.X.deploy.resources.limits`) naming the very key whose absence is the problem, and the useful line to mark is `X:`. Unlocatable findings keep `Line: 0`, which the UI reports separately rather than mis-anchoring.
  - **Constraints on `POST /api/custom/lint/compose`** (`internal/routes/lint_routes.go`) — a lint report is derived from repo content and a worker policy, so the route has to stay narrow. Keep these when changing it: `compose_path`/`compose_file` go through `safepath` before anything else (traversal, absolute paths and non-YAML extensions are rejected; a dash-leading filename is safe because `-f` takes its value positionally, not as a flag). An optional `stack` must belong to the given `repository` — otherwise the undefined-variable rule becomes an oracle for enumerating another stack's variable names, and the route would resolve that stack's Vault/Infisical secrets as a side effect. An unknown `worker` is rejected rather than accepted, because `policy.Load` falls back to the *global* policy for a worker it cannot find, which would report a clean bill of health for a policy that was never consulted. Every one of those checks runs *before* the git clone/fetch, so a request that will be refused costs no network round trip. `config_error` keeps the compose diagnostic (that is the point of the endpoint) but runs through `lint.RedactWorkspacePaths` so the server's data-directory layout does not travel with it — the stacks create hook's own lint rejection reuses the same function for the same reason. The request is bounded by `lintTimeout` and `apis.BodyLimit`. Findings never carry env var *values* or the contents of a policy allowlist — only names, and the same violation text a deploy would have produced.
- **SOPS+age secrets** (`internal/integrations/sops/`, `internal/secrets/sops.go`): a third, distinct secrets mechanism alongside the per-env-var `internal/secrets` providers (internal/vault/infisical) above — file-based and repo-scoped rather than per-variable. Each repository gets an auto-generated age keypair (`repositories.sops_age_key`\* / `sops_age_public_key`, keygen in `internal/hooks/pb_hooks.go` on repo create). `internal/sync/reconciler.go` auto-decrypts a `secrets.yaml` found next to `wireops.yaml` and overlays its values on top of the stack's env vars during sync, rollback, redeploy, and transfer — no explicit provider selection per key. Registered as a locked/always-enabled `Secret Backend` integration (slug `sops`) that can't be toggled off. Key rotation (`POST /api/custom/repositories/{id}/sops-rotate-key`) is explicit and destructive-ish: it invalidates any `secrets.yaml` encrypted for the old public key until re-encrypted.
- **Audit log** (`internal/audit/audit.go`): records custom-route requests and system events (actor, action, resource, status, origin) with a configurable retention window, purged by a periodic sweep. Exposed via `GET /api/custom/audit-logs` (filterable by `from`/`to`/`actor_type`/`actor_id`/`action`/`resource_type`/`resource_id`/`origin`/`status`).
- **Secrets providers** (`internal/secrets/`): pluggable `SecretProvider` registry, `ValidProviders = ["internal", "vault", "infisical"]`, all three implemented and functional. `internal` encrypts the value at rest (AES-GCM, `SECRET_KEY`). `vault`/`infisical` store a reference string (`<mount>/data/<path>#<field>` / `<project-id>/<environment>/<secret-path>#<SECRET_NAME>`) resolved at deploy time against a backend configured via the `integrations` collection (category "Secret Backend"), not server env vars. Once an env var is saved as a secret its `secret_provider` is immutable (`preventEnvSecretProviderChange` in `internal/hooks/pb_hooks.go`) — must delete/recreate to switch backends. `internal/envvars/backend_check.go` pre-flight-blocks stack/job execution if a referenced backend is disabled, naming the provider and offending keys.
- **OIDC / SSO** (`internal/oidc/collection.go`): PocketBase collection glue for OIDC client secret hydration; see README's OIDC env var table for the user-facing setup and the SSO role-override warning.
- **Setup/bootstrap** (`internal/setup/service.go`): backs the `/setup` first-admin flow gated by `BOOTSTRAP_TOKEN`.
- **Backup** (`internal/backup/`): wraps `core.App.CreateBackup`/`RestoreBackup` behind wireops RBAC (`GET/POST/DELETE /api/custom/backups`, `POST /api/custom/backups/upload` gated to a real PocketBase superuser, `POST /api/custom/backups/{key}/restore`). PocketBase always manages *local-disk* backups only; optional off-host mirroring is the `"s3"` `internal/integrations` entry (Category "Storage Backend", config/credentials in the `integrations` collection like Vault/Infisical) — `app.OnBackupCreate()` uploads each new local backup via `internal/backup/remote` (a provider-agnostic `Storage`/`KeyManager` registry, S3-only today) as a **replica**, not a move: the local copy is never deleted by mirroring. `List` merges local disk with the remote listing, flagging each backup `Remote` if it also exists there (`Info.Remote`); `Delete` removes both copies; `Restore` prefers the local copy and only re-downloads from remote if it's missing locally. Content is AES-256-GCM-encrypted before upload by default (SECRET_KEY-derived key, or an AWS KMS-wrapped data key if configured). Mirror attempts (once remote storage is actually enabled) are audited (`backup.mirror` in `audit_logs`) and failures notify via `notify.BackupMirrorError`.

---

## Coding Conventions

- All custom API handlers are in `internal/routes/`; each file groups a domain.
- Secrets are always encrypted before persistence via `internal/crypto/encrypt.go`; never store plaintext.
- WebSocket protocol message types are defined in `internal/protocol/messages.go` and shared by both server and worker.
- PocketBase schema changes are Go migration files in `pb_migrations/`; always add a new numbered file, never modify existing ones.
- Path safety for user-supplied file paths must go through `internal/safepath/`.
- Frontend pages live in `frontend/app/pages/` (Nuxt file-based routing); shared composables in `frontend/app/composables/`.
- Test function names must be in CamelCase.
- **Vue Test Utils stubs must not use inline HTML-string `template:` fields.** Codacy's static analysis flags any `template: '<...>'` string literal as a generic XSS sink ("Non-HTML variable used to store raw HTML"), even in test-only mock components with no user input. Write stub components with `setup()` + `h()` from `vue` instead — see `frontend/app/components/_tests/WorkerEnvBadges.test.ts` and `WorkerSystemInfoCard.test.ts` for the pattern. This avoids the false positive entirely rather than suppressing it.

### Frontend DOs and DON'Ts (accessibility, concurrency, testing)

Recurring review findings distilled into rules — check new components/pages against these before calling frontend work done.

**Markup / a11y:**
- **DON'T** nest flow-content elements (`div`, another heading's icon wrapper, a component whose root is a `div`) inside phrasing-content-only elements (`h1`–`h6`, `p`, `button` label, `span`). **DO** use `span`/`inline-flex` for icon wrappers, badges, or lists rendered inside a heading — check the child component's own root tag too, not just the call site.
- **DON'T** ship `target="_blank"` links with no indication they leave the page. **DO** add an `aria-label` (or visually-hidden text) noting it opens in a new tab.
- **DON'T** make explanatory text reachable only via hover (`UPopover mode="hover"`, tooltip-only content). **DO** also set a `title` attribute on the trigger, or use a keyboard-focusable element, so keyboard/AT users get the same info.
- **DON'T** render a static separator/prefix (e.g. `/`) unconditionally next to optional data. **DO** guard the separator with the same `v-if` as the data it separates.

**State & concurrency:**
- **DON'T** disable only the specific button/card a user clicked when guarding against a concurrent async action (OAuth popups, in-flight requests). **DO** disable every sibling trigger off the shared in-flight flag (e.g. `connectingSlug`) so two flows can't run at once and corrupt shared form state.
- **DON'T** just `navigateTo()` after a delete action. **DO** close the confirmation modal first, then navigate with `{ replace: true }` so the back button doesn't return to the now-deleted resource (see `jobs/[id].vue`'s delete flow).

**Testing:**
- **DON'T** rely on Nuxt's auto-import for Vue composition APIs (`nextTick`, `computed`, `ref`, `watch`, …) in a component you intend to unit-test with plain Vue Test Utils. **DO** import them explicitly from `vue` — auto-import doesn't exist outside the Nuxt runtime, so an unimported `nextTick` works in the app but throws in tests.
- **DON'T** use Vue Test Utils' `isVisible()` to assert `v-show` state in this repo — happy-dom's `getComputedStyle` doesn't reliably resolve inline `style="display: none"` here. **DO** read `element.style.display` directly.
- **DON'T** leave a new component's nested children (icons, sub-components) unregistered in a test when they sit on the actual assertion path — an unresolved child just silently renders nothing, so a real regression there passes unnoticed. **DO** register the real component (or an equivalent `h()`-based stub) for anything the assertions actually depend on.
- **DON'T** ship a new wizard/step-machine/filtering component without Vitest coverage of its main flow (happy path end-to-end) and its edge cases (empty/fallback state, validation blocking submit).

## Testing & Coverage

- Backend: `go test -coverprofile=coverage.out $(go list ./... | grep -v '^github.com/wireops/wireops/worker\(/\|$\)')` (excludes `worker/...`, run separately). Current baseline: ~29% statement coverage overall — uneven across packages (e.g. `internal/config`, `internal/webhook`, `internal/setup` are near/at 100%; `internal/routes`, `internal/sync`, `internal/compose` are thin).
- Frontend: `npm run test:coverage` (vitest + `@vitest/coverage-v8`, reports `text` + `lcov` to `frontend/coverage/`). Current baseline: ~62% statement coverage.
- Both are uploaded to Codacy via `.github/workflows/quality-codacy.yml` (`continue-on-error: true` — informational, not a merge gate).
- **Minimum targets** (hobby/side-project pace, not enforced by CI — treat as a floor when touching a package, not a blanket requirement to backfill):
  - Backend: **25%** overall statement coverage. New/changed `internal/*` packages with non-trivial logic (parsing, reconciliation, auth/rbac, encryption) should carry tests; thin glue code (routes wiring, migrations) is exempt.
  - Frontend: **50%** overall statement coverage. Prioritize composables and utils (`frontend/app/composables/`, `frontend/app/utils/`) over component markup.
- These are floors, not aspirational targets — raise them only once coverage comfortably clears them for a few months.
- **Agent directive**: treat coverage as something to actively grow, not just avoid regressing. When you touch a package, look for nearby untested branches (error paths, edge cases) and add tests even if not strictly required by the floor.
- **Agent directive — generated code**: whenever you generate new functions, handlers, composables, or components, think about how to make that code testable and write the corresponding test in the same change (table-driven Go tests for backend logic, vitest for frontend composables/utils). Don't ship new non-trivial logic without at least one test exercising its main path and one obvious edge case.

## Git Workflow

- **Never commit directly to `main`.** Always create a feature branch and open a PR, even for one-line fixes.
- Push straight to `main` only if the user explicitly asks for it in that specific request — a prior approval does not carry over to later work.
