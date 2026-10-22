# wireops — Agent Context

Self-hosted GitOps platform for managing Docker Compose stacks and scheduled Docker-based jobs. It watches Git repositories for changes and deploys updates on remote hosts through token-authenticated WebSocket workers.

**Scope**: targets developers, homelabs, and self-hosters running plain `docker compose` stacks across one or many hosts/repos — without manifest sprawl or enterprise-grade autoscaling/control-plane complexity. **Not** a Kubernetes/Swarm alternative, not a competitor to Flux/ArgoCD/enterprise app-management platforms; cluster orchestration, multi-node scheduling, and autoscaling are explicitly out of scope. For production/internet-exposed workloads at enterprise scale, point users toward Kubernetes (Flux/ArgoCD) instead of stretching wireops into that role.

**Project status**: pre-1.0 (current release line `v0.2.x`, latest at this update `v0.2.11`), active hobby/side-project pace. Core GitOps sync, worker deploy security policy, and RBAC/audit are implemented and in daily use. `internal/secrets` has all three providers implemented and functional: `internal` (AES-GCM), `vault` (HashiCorp Vault), and `infisical`. `internal/backup` (create/list/upload/delete/restore, cron autobackup) is implemented and shipped; optional off-host mirroring to S3-compatible storage is the "s3" `internal/integrations` entry (see `internal/backup/remote`). Private registry credentials shipped in `v0.2.11`. Don't describe stub features as shipped in docs or responses without checking the source first.

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
- [MCP Server](#mcp-server)
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
│   ├── manifest/parser.go        # Parses declarative stack config: embedded `x-wireops` block (primary) or standalone `wireops.yaml` (deprecated)
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
├── mcp/                          # Standalone MCP server binary (wireops-mcp) — read + operator-scoped write tools, generate/scaffold tools, pass-through auth
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
| Backend language | Go 1.27 |
| Backend framework | PocketBase v0.40 (embedded SQLite, REST, realtime SSE) |
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
| `stacks` | `name`, `repository`, `compose_path`, `auto_sync`, `status`, `worker`, `current_version`, `render_overrides` (JSON, per-service image/ports/networks/scale not committed to git) |
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
1. `sync/scheduler.go` polls each stack on an interval set by `SCAN_PERIOD` (default 10s, `internal/config.GetScanPeriod`); a stack's own positive `sync_interval_seconds` (from `wireops.yaml`'s `sync.interval`, or an embedded `x-wireops` block's `sync.interval` — see below) overrides this fallback for that stack.
2. `sync/reconciler.go` runs `git.CloneOrFetch` and compares the latest commit SHA with the stored one.
3. `sync/lint.go` runs `internal/lint` over the resolved compose config and records a `lint` phase on the deploy timeline. This run is advisory — it never aborts the reconcile itself, it just gives the timeline a lint summary early. The renderer (step 4) runs its own `lint.Run` after applying render overrides and *does* abort the deploy on any error-severity finding, alongside the worker-policy check it already ran.
4. `sync/renderer.go` reads the compose YAML, injects `dev.wireops.*` labels, and writes a versioned revision file to `DATA_DIR/stacks/<id>/v<n>.yml`.
5. The server base64-encodes the compose file, sends a `DeployCommand` over WebSocket to the worker assigned to the stack, and waits up to 5 minutes for `CommandResult`.
6. The worker decodes the compose file and executes `docker compose up`.
7. `sync/postcheck.go` queries live container state and derives the stack's post-deploy status (`active`/`degraded`/`error`) via `evaluatePostCheck`. A service labeled `customization.init: "true"` is treated as run-to-completion: exiting 0 (or being gone entirely) counts as healthy, so a one-shot migration/seed container doesn't degrade the stack the way a crashed long-running service would — only a non-zero exit or a restart loop does.
8. Persists a `sync_logs` entry, updates stack status, fires webhook/ntfy notification.

### Single-file stacks (`x-wireops`) — the primary way to define a stack
A stack is normally driven by a compose file alone, not a compose file plus a separate `wireops.yaml`. A top-level `x-wireops:` block in the compose file carries the exact same fields as standalone `wireops.yaml` (`version`, `name`, `group`, `timeout`, `compose.remove_orphans`/`force_pull`, `jobs.wait_running`, `worker.tags`, `sync.interval`) — `internal/manifest.ParseComposeManifest` parses it with the same `manifest.Definition`/`Validate()` used for the standalone file. `x-*` is a [compose extension field](https://docs.docker.com/reference/compose-file/extension/) that `docker compose` itself ignores, so the file stays a valid, directly-runnable compose file. The separate-`wireops.yaml` two-file layout (`/stacks/from-wireops`) still works unchanged and stays fully supported, but is **deprecated** — prefer `x-wireops` for anything new.
- Create via `POST /stacks/from-compose` (`repository`, `worker`, `compose_path`, `compose_file`); the resulting stack gets `config_source: compose_embedded` and the same field-immutability treatment as `config_source: wireops_file` (`internal/hooks/pb_hooks.go`, `internal/sync/reconciler.go`).
- `sync/renderer.go` strips the `x-wireops` key from the resolved config before writing the versioned revision file — `docker compose config` passes `x-*` keys through verbatim, but it has no meaning to `docker compose up` and must never reach the worker.
- No overlap with anything wireops injects at the service level: `x-wireops` is a top-level compose key, while every server-managed identifier (`dev.wireops.managed`, `dev.wireops.stack_id`, `dev.wireops.repository.*`, …) and the user-facing `dev.wireops.config.<name>` mount annotation live under `services.<name>.labels`/`annotations`. Different altitude, same reserved namespace, zero collision.
- **MCP asserts this as the default.** `mcp/tools`' `scaffold_stack` tool embeds `x-wireops` and returns a single compose file unless the caller explicitly passes `two_file: true` to opt into the legacy layout; `mcp/prompts`' `scaffold_new_stack` prompt follows the same default and points the model at `from-compose`. `generate_wireops_yaml` (standalone `wireops.yaml` only) is documented as the deprecated-layout tool.
- Discovery/preview mirrors the `wireops.yaml` flow: `GET /api/custom/repositories/{id}/compose-wireops-files` lists candidate compose files (sniffed for a top-level `x-wireops` key), `GET /api/custom/repositories/{id}/compose-definition?file=` parses and returns one file's `x-wireops` block as a `Definition`, same shape as `/wireops-definition`.

### Worker Bootstrap & Communication
1. Admin generates a token via the UI/API.
2. Worker connects to `POST /worker/register` (using the HTTPS endpoint on port :8443) with the `X-Wireops-Worker-Token` header to register.
3. The server validates the token (transitioning it from `STAGING` to `ACTIVE`) and associates it with the worker.
4. Worker opens a persistent WebSocket connection to `/worker/ws` on the worker server, authenticated via the same token.
5. Server dispatches typed commands (`DeployCommand`, `TeardownCommand`, `RunJobCommand`, etc.) as JSON `Envelope` messages.
6. Worker sends heartbeats every 30s; job completions are pushed as unsolicited `MsgJobCompleted` messages.
7. If a worker is offline when a change is detected, a `stack_pending_reconciles` record is created and replayed on reconnect.
8. Workers advertise what they can do in `WorkerInfo.Capabilities` (`worker/telemetry`), persisted on the `workers` record. The server checks one before dispatching a command an older worker would never answer — `project_identity_migration_v1` and `exec_one_shot_v1` today. Add a capability string for any new command rather than assuming the fleet is up to date; `MsgExec` returns a `409` naming the missing one so the caller can fall back.

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
| `PUT` | `/stacks/{id}/render-overrides` | Set per-service image/ports/networks/scale overrides (not committed to git); gated by `allow_render_overrides` worker policy; image/ports/networks changes force-recreate, scale-only changes adjust replicas in place |
| `DELETE` | `/stacks/{id}/render-overrides` | Clear render overrides; force-recreates |
| `GET` | `/stacks/{id}/stream` | SSE log stream. Unnamed `data:` frames carry output lines; named `event: status` frames carry `{record_id, status}` on every `sync_logs` status transition seen live (stored rows seed the status silently, so a reconnect cannot be mistaken for a fresh outcome). A 3s poll of the newest row recovers a transition whose broker event was dropped — `logstream.Publish` skips a subscriber whose 16-slot buffer is full, and a deploy publishes one event per output line, so the event carrying the terminal status is exactly the kind that can be lost. Output needs no such recovery: it is cumulative, so the next delivered event resends what was missed. `?history=false` streams only what happens from now on |
| `GET` | `/stacks/{id}/container/{cid}/stats` | CPU/mem stats |
| `GET` | `/stacks/{id}/container/{cid}/logs` | Container logs |
| `POST` | `/stacks/{id}/container/stop` | Stop a container |
| `POST` | `/stacks/{id}/container/restart` | Restart a container |
| `POST` | `/stacks/{id}/exec` | Run one non-interactive command in a container (`container_id`, `cmd` argv, optional `working_dir`/`user`/`timeout_seconds`) and return `{stdout, stderr, exit_code, timed_out}`. `CapUseTerminal`, audited as `stack.exec` with the argv. `409` (naming `exec_one_shot_v1`) when the worker is too old — callers fall back to the terminal session routes |
| `GET` | `/stacks/import/discover` | Discover unmanaged Compose projects |
| `POST` | `/stacks/import` | Import a local Compose stack |
| `POST` | `/stacks/from-compose` | **Primary way to create a stack from a file.** Request body carries `repository`, `worker`, `compose_path`, `compose_file`; the stack's deploy-behavior fields (env, cron, etc.) are re-parsed server-side from the top-level `x-wireops` block embedded in that compose file, never trusted from the request. See `internal/manifest.ParseComposeManifest` and "Single-file stacks" below. Sets `config_source: compose_embedded`; those fields become immutable via the API afterward (`internal/hooks/pb_hooks.go`). |
| `POST` | `/stacks/from-wireops` | **Deprecated** two-file equivalent of `from-compose`: creates a stack from a separate `wireops.yaml` found in a repo (`repository`, `worker`, `wireops_file`) instead of an embedded `x-wireops` block. Sets `config_source: wireops_file`, same re-parse-server-side and immutability rules. Success response includes `"deprecated": "true"`. Still fully supported; prefer `from-compose` for anything new. |

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

### Worker Policy (reads `CapViewWorkerPolicy`/operator, writes `CapManageSettings`/admin; not superuser-only)
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/custom/workers/{id}/policy` | Resolved effective deploy security policy for a worker, plus its local overrides (`CapViewWorkerPolicy`) |
| `PUT` | `/api/custom/workers/{id}/policy` | Set per-worker policy overrides (`CapManageSettings`) |
| `DELETE` | `/api/custom/workers/{id}/policy` | Clear per-worker overrides (revert to inherit) (`CapManageSettings`) |
| `GET` | `/api/custom/settings/worker-policy` | Global `worker_policies` singleton (`CapViewWorkerPolicy`) |
| `PUT` | `/api/custom/settings/worker-policy` | Update global `worker_policies` singleton (`CapManageSettings`) |

The reads sit at `CapViewWorkerPolicy` (minimum role `operator`) on purpose: a
service account caps at `operator` (`internal/routes/service_accounts.go`), so
with `CapManageSettings` on the GETs an API key could deploy a compose file but
never read the policy that decides whether it would be rejected. Writing a
policy is still an admin settings change.

### Audit (`admin` capability — `CapViewAuditLogs`)

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/custom/audit-logs` | Filterable audit log query (from/to/actor/action/resource/origin/status) |

### Identity (any authenticated principal — no capability gate)

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/custom/me` | The caller's own `role`, resolved `capabilities[]`, actor type/id, plus `version`/`commit`/`build_date` and `scan_period_seconds` |

Deliberately ungated (`internal/routes/me.go`): it reports only on the caller,
and the one call a client makes to find out what it may do must not itself be
able to 403. Anonymous requests get 401, a disabled user 403. The capability
list comes from `rbac.CapabilitiesFor`, derived from the same
`minimumRoleByCapability` table `rbac.Require` enforces, so it cannot drift.

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

### MCP Server
| Variable | Description |
|---|---|
| `SERVER_URL` | **Required.** Base URL of the wireops server the MCP process proxies to |
| `MCP_LISTEN_ADDR` | Listen address for the streamable-HTTP endpoint (default: `:8091`) |
| `MCP_TOOLS` | Which tool groups to register: comma-separated `read`, `write`, `exec`, or `all` (default: `all`). A deployment used only for diagnosis registers `read` and halves what every session pays for `tools/list`. Not an authorization control — the server's RBAC still decides every call; an unknown group is a startup error, not a silent omission |

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

## MCP Server

A third deployable (`mcp/`, image from `Dockerfile.mcp`, binary `wireops-mcp`) that translates the existing REST API into MCP. It is **not** mounted into the server — it is its own process, listening on `MCP_LISTEN_ADDR` (default `:8091`) and talking to `SERVER_URL`. Transport is streamable HTTP at `POST/GET /mcp` (`mcp/app/app.go`), via `github.com/modelcontextprotocol/go-sdk`. 64 tools in three groups, 3 resource templates (`wireops://stacks/{id}/compose`, `wireops://jobs/{id}/raw`, `wireops://stacks/{id}/logs/live`, the last subscribable through the SSE bridge in `mcp/resources/livelogs.go`), 1 static resource (`wireops://conventions/compose`) and 2 prompts.

- **Pass-through auth, no credential of its own.** Each client supplies its own `service_accounts` API key per connection (`X-Wireops-Api-Key`, or `Authorization: Bearer`), stashed in the request context by `withAPIKeyMiddleware` and forwarded verbatim by `mcp/client`. Nothing is persisted, so the audit log keeps per-agent granularity and a key can be revoked individually. `mcp/client`'s `CheckRedirect` refuses any redirect off `SERVER_URL`'s origin, since Go would otherwise carry the key header to it.
- **Authorization is the server's, never the MCP process's.** There is no per-tool capability check and no allowlist: every tool in the selected `MCP_TOOLS` groups is registered for every session regardless of the caller's role, and a key that is not `operator` gets a 403 from `rbac.Require(...)` at call time. Deliberate — a gate here would be a second, weaker copy of the RBAC. Since `internal/routes/service_accounts.go` caps a service account at `viewer`/`operator`, the MCP ceiling is exactly the `operator` capability set (`operate_stacks`, `manage_repositories`, `manage_jobs`, `manage_workers`, `use_terminal`).
  - **No tool may call a route an API key cannot reach.** A service account caps at `operator`, so a `manage_settings`/`view_audit_logs` route always 403s for it — a tool over one is worse than absent, because the agent tries it. `list_audit_logs`, `list_integrations`, `list_orphans` and `get_system_info` were therefore removed; `get_worker_policy`/`get_global_worker_policy` stayed and now work, because those two GETs moved to `CapViewWorkerPolicy` (operator, see § Worker Policy), which is also what makes `scaffold_stack`'s `worker_id` check usable. Keep this rule when adding a tool: check the route's capability against `rbac.MinimumRole` first.
- **`whoami`** is the first tool and the one every permission error points at: `GET /api/custom/me`, giving the key's role, its capabilities, and the instance's version plus `scan_period_seconds`. `mcp/tools`' `fetchWhoami` caches it per API key for 5 minutes (`mcp/tools/cache.go`) because the stack listings need `scan_period_seconds` to resolve an inherited sync interval — never cache it across keys, the value names the caller's own permissions.
- **Tool groups** (`MCP_TOOLS`, `tools.ParseToolSet`): `read` (every tool annotated `readOnly()`, wherever it lives — `registerReadTools` plus `registerStackReadTools`/`registerRepoReadTools`/`registerJobReadTools`, which exist precisely so `lint_compose`, `preview_stack_migration` and the repo browsers are in the read set), `write` (the four write registrars), `exec`. A tool's group must follow its annotation; a test asserts every tool in the read set is `readOnly()`.
- **Names are accepted wherever an id is** (`mcp/tools/refs.go`): `resolveRef` passes a `^[a-z0-9]{15}$` ref through untouched (no round trip) and otherwise looks it up by the collection's name field, reusing `confirmNameField`. Zero matches and ambiguous matches are *errors naming the candidates*, never a guess. Values reach a PocketBase filter, so they go through `client.EscapeFilterValue`. Add it to any new tool that takes an id, and keep `requireConfirm` matching against the resolved record.
- **Prose belongs in the output, not the description** (`appendNotice`, `statusNotice`, `serviceStateNotices`, `renderOverridesNotice`): a tool description is paid for by every session at `tools/list`, a `_notice` only when the case actually occurs. `get_stack_status`/`list_stacks` explain `paused`/`degraded` and render overrides that way; `get_stack_services` the `is_init`/null-`exit_code` semantics; `get_stack_compose` names the `${VAR}`s nothing defines (wireops renders with `--no-interpolate`, so the worker turns an undefined one into an empty string silently); `list_secrets` says an empty secret list is not an empty env; the repo file listings say `registered:false` means "not a stack yet"; `scaffold_stack` points at `wireops://conventions/compose`, which is the single copy of the label/annotation conventions (previously duplicated in that description *and* the `scaffold_new_stack` prompt).
- **`wait` on `deploy_stack`/`force_redeploy_stack`/`rollback_stack`** (`mcp/tools/wait.go`): subscribes to `GET /api/custom/stacks/{id}/stream?history=false`, *then* triggers, then returns the terminal `sync_logs` status with the stack status and the deploy log. The order is the whole trick — the trigger routes are fire-and-forget (`scheduler.TriggerSync`), so a status published between trigger and subscribe is gone. `client.Stream`'s `onOpen` fires on the 2xx, not on the first event, because the first event *is* the one being waited for; waiting on an event there deadlocks. The wait cannot prove the outcome it reports belongs to the reconcile it triggered: the stream is per stack, the trigger routes return nothing, and `ReconcileStack` skips a trigger for a stack that is already syncing. It therefore reads the newest `sync_logs` id *before* triggering and, when the terminal status turns out to be that same row, says so in the `_notice` rather than passing another reconcile's result off as this call's; the no-frame fallback refuses that row outright. A real fix needs the trigger routes to return an operation id and the status frames to carry it. `terminalSyncStatuses` must stay in step with the `sync_logs.status` select (`pb_migrations` 01/40/43): `success`/`error`/`noop`/`degraded` end a reconcile, while `running`/`queued`/`waiting_jobs` do not. A status missing from that map is not a cosmetic bug — the wait runs to its timeout and reports `timed_out` for a reconcile that finished, and `noop` (rendered output unchanged) is the normal outcome of `deploy_stack` when git has not moved.
- **Tool annotations** (`mcp/tools/annotations.go`): every tool carries `ToolAnnotations` via one of `readOnly()` / `additive(idempotent)` / `destructive(idempotent)`, because the MCP default for an unannotated tool is `destructiveHint=true`. This is what lets a client decide unattended-vs-confirm. Keep new tools annotated; `lint_compose` and `preview_stack_migration` are `readOnly()` despite being POSTs, since they only parse.
- **Confirm gate** (`mcp/tools/confirm.go`): the tools whose effect cannot be undone take a `confirm` field and run `requireConfirm`, which resolves the record's own name server-side and demands an exact match — `stacks`→`name`, `workers`→`hostname`, `repositories`→`name`, `scheduled_jobs`→`name`; `stop_container` matches the resolved compose service name and `delete_job_run` the literal `"delete"`. The error quotes the expected value on purpose, so the agent relays it and asks rather than retrying. `force_redeploy_stack` and `set_stack_env_vars`/`set_job_env_vars` require it only on their destructive branch (`recreate_volumes`, `mode: "replace"`). A mismatch must fail **before** any write request leaves the process — the tests assert exactly that. `matchConfirm` also refuses an *empty* expected value, since `""` would otherwise match an omitted `confirm`: `resolveContainerTarget` returns an empty service name for a container without a `com.docker.compose.service` label.
- **Write tools map 1:1 onto routes the UI already calls**, so RBAC, worker-policy validation and audit entries all come for free. Pause/resume is a `PATCH` on the `stacks` record (`set_stack_paused`), the same mechanism `frontend/app/pages/stacks/[id].vue` uses — and, like the UI, resume is only allowed from `paused`: `status` also carries `error`/`degraded`/`syncing`, and blindly writing `active` would replace a real status with a healthy-looking one that nothing recomputes until the next reconcile; collection CRUD (`repositories`, `scheduled_jobs`) is audited by the global `OnRecord{Create,Update,Delete}Request` hooks in `internal/hooks/pb_hooks.go`.
- **`exec_in_container`** (`mcp/tools/tools_exec.go`) calls `POST /api/custom/stacks/{id}/exec`, which dispatches `MsgExec` to the worker and comes back with separated stdout/stderr and the container runtime's own exit code. It takes either `command` (wrapped in `sh -c`) or `argv` (run directly, for an image without a shell).
  - **The pty fallback** (`execViaTerminal`) is kept only for a worker that does not advertise `protocol.CapabilityExecOneShotV1`, which the route reports as a `409`: it opens a real pty session, sends the command followed by a `printf` that emits a per-session end-of-command marker plus `$?`, polls the asciicast-v2 transcript until that marker appears (or the deadline passes), then closes the session in a `defer` so an abandoned shell never waits for the 15-minute idle sweeper. Transcript events are base64-encoded (see `internal/termsession.AppendTranscript`), and a torn final line is skipped rather than failing the read. Transcript ownership works because `actorID` is the service-account id, so the same key that opened the session can read it back. Only a 409 falls back — any other status is the server's answer and is surfaced.
  - **Don't replace the marker with a quiet period** *in that fallback*. The pty has `ECHO` on (`worker/executor/terminal.go` opens the exec with `Tty: true`), so the transcript grows with the echoed command line before the command has printed anything — waiting for the output to stop growing reports any command that is silent for a moment before its first byte (`du -sh /`, a slow DNS lookup) as finished with no output and `timed_out: false`. The marker is passed to `printf` as an *argument*, which is what lets `awaitTranscriptOutput` tell the echoed input line (marker next to a quote) from the real end of output (marker next to `:<status>`); `stripEchoedInput` then drops the echo. The fallback's response says in a `_notice` that its streams are interleaved, because they are.
- **Deliberately not exposed**, though an operator could call them: credential/registry `test` and `keyscan`, the git-provider OAuth routes, `sops-rotate-key`, the Vault/Infisical browse routes, and repository credential writes (`repository_keys`). Setup/secret-material flows that belong in the UI.
- **Tests**: `go test ./mcp/...` (own CI workflow, `.github/workflows/mcp-ci.yml`, plus `govulncheck`). Pattern is stdlib-only — `httptest.NewServer` plus the handler called as a plain function with `ctxWithKey()`; `mcp/tools/tools_write_test.go`'s `recorder` records every request so a test can assert the method, path, query, body *and* that nothing was written at all.

---

## Access Control, Policy & Audit

- **RBAC** (`internal/rbac/rbac.go`): four roles — `viewer` < `operator` < `admin`, plus a separate `monitoring` role scoped to metrics-only access. Capabilities (`CapViewStacks`, `CapOperateStacks`, `CapViewWorkerPolicy`, `CapManageSettings`, `CapManageSecurity`, `CapViewAuditLogs`, …) map to a minimum required role; route handlers check capabilities, not raw role strings. `AllCapabilities()`/`CapabilitiesFor(role)` expose that table for introspection (`GET /api/custom/me`) — derived from `minimumRoleByCapability`, never a second copy of it. `Require`'s 403 body carries `required_capability`/`minimum_role`/`actor_role` next to the `"permission denied"` message, so a client can say *what* is missing; the message text itself is matched by `frontend/app/utils/error-parser.ts` and must stay verbatim.
- **Worker deploy policy** (`internal/policy/`): resolves an effective `WorkerPolicy` per worker from the `worker_policies` singleton (global) with optional per-worker overrides. Enforces allowlists (volumes/networks/images/cap-add/devices/security-opt) and boolean blocks (`BlockPrivileged`, `BlockHostNetwork`, `BlockHostPID`, `BlockHostIPC`, `BlockDockerSocket`, `PreventLatestImages`, `BlockHostVolumes`), plus `AllowRenderOverrides` (off by default) gating whether a stack's `render_overrides` (per-service image/ports/networks/scale, not committed to git — see `internal/sync/renderer.go`'s `ApplyServiceOverrides`) may be applied at all; overridden values still pass through the same allowlist/block checks after being applied. Empty allowlist = open; first entry added = allowlist-only from then on. `policy_inherit` controls whether a worker without a local override falls back to the global value. Two entry points: `ValidateComposeConfig` returns the first violation and is what blocks a deploy (renderer, render-overrides route); `ComposeViolations` returns all of them, structured per service, and is what `internal/lint` reports. Both run off the same extraction and produce identical messages — keep it that way when adding a check.
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
- **Backup** (`internal/backup/`): wraps `core.App.CreateBackup`/`RestoreBackup` behind wireops RBAC (`GET/POST/DELETE /api/custom/backups`, `POST /api/custom/backups/upload` gated to a real PocketBase superuser, `POST /api/custom/backups/{key}/restore`). The built-in archive covers `PB_DATA_DIR` (default `DATA_DIR/pb_data`), not its default `DATA_DIR/repos` and `DATA_DIR/stacks` siblings; those Git working copies and rendered artifacts are recreated later or require a separate full-data snapshot. PocketBase always manages *local-disk* backups only; optional off-host mirroring is the `"s3"` `internal/integrations` entry (Category "Storage Backend", config/credentials in the `integrations` collection like Vault/Infisical) — `app.OnBackupCreate()` uploads each new local backup via `internal/backup/remote` (a provider-agnostic `Storage`/`KeyManager` registry, S3-only today) as a **replica**, not a move: the local copy is never deleted by mirroring. `List` merges local disk with the remote listing, flagging each backup `Remote` if it also exists there (`Info.Remote`); `Delete` removes both copies; `Restore` prefers the local copy and only re-downloads from remote if it's missing locally. Content is AES-256-GCM-encrypted before upload by default (SECRET_KEY-derived key, or an AWS KMS-wrapped data key if configured). Mirror attempts (once remote storage is actually enabled) are audited (`backup.mirror` in `audit_logs`) and failures notify via `notify.BackupMirrorError`.

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
- **DON'T** build an icon name dynamically (`` `i-lucide-${status}` ``, string concatenation, a name coming from the API). **DO** write the full literal (`'i-lucide-check'`) in a `.vue`/`.ts` file under `frontend/app/`. Icons are inlined at build time by `@nuxt/icon`'s source scanner (`icon.clientBundle.scan` in `nuxt.config.ts`) and the Iconify API fallback is off (`provider: 'none'`), because wireops runs self-hosted on hosts with no outbound internet — a name the scanner can't see statically renders as a blank box in production even though it works in `nuxt dev`.

**State & concurrency:**
- **DON'T** disable only the specific button/card a user clicked when guarding against a concurrent async action (OAuth popups, in-flight requests). **DO** disable every sibling trigger off the shared in-flight flag (e.g. `connectingSlug`) so two flows can't run at once and corrupt shared form state.
- **DON'T** just `navigateTo()` after a delete action. **DO** close the confirmation modal first, then navigate with `{ replace: true }` so the back button doesn't return to the now-deleted resource (see `jobs/[id].vue`'s delete flow).

**Testing:**
- **DON'T** rely on Nuxt's auto-import for Vue composition APIs (`nextTick`, `computed`, `ref`, `watch`, …) in a component you intend to unit-test with plain Vue Test Utils. **DO** import them explicitly from `vue` — auto-import doesn't exist outside the Nuxt runtime, so an unimported `nextTick` works in the app but throws in tests.
- **DON'T** use Vue Test Utils' `isVisible()` to assert `v-show` state in this repo — happy-dom's `getComputedStyle` doesn't reliably resolve inline `style="display: none"` here. **DO** read `element.style.display` directly.
- **DON'T** leave a new component's nested children (icons, sub-components) unregistered in a test when they sit on the actual assertion path — an unresolved child just silently renders nothing, so a real regression there passes unnoticed. **DO** register the real component (or an equivalent `h()`-based stub) for anything the assertions actually depend on.
- **DON'T** ship a new wizard/step-machine/filtering component without Vitest coverage of its main flow (happy path end-to-end) and its edge cases (empty/fallback state, validation blocking submit).

### Backend DOs and DON'Ts (secret handling, audit)

- **DON'T** start a detached, fire-and-forget goroutine (`go func`, `sync.WaitGroup.Go`) without a `recover` inside it. **DO** recover at the top of the goroutine and log, downgrading the line when `ctx.Err() != nil` since a panic racing shutdown is expected — see `sync.Scheduler.safeRun` and `notify.Notifier.Dispatch`. A caller's own recover does not contain a goroutine it spawned, and `sync.WaitGroup.Go` repanics after `Done`, so one panic in a background notification takes down the whole server mid-reconcile. This has already cost one CI flake (`internal/routes` crashing on a notify dispatch that outlived the test app).
- **DON'T** return decrypted secret/plaintext values from a custom route without `Cache-Control: no-store` on the response. **DO** set it before every `e.JSON(...)` call that carries plaintext (see `revealEnvVar`/`revealStackEnvVars` in `internal/routes/env_var_routes.go`) — otherwise an intermediate cache or browser history entry can retain the decrypted value outside the server's control.
- **DON'T** collapse a bulk/multi-record action that discloses secrets into one aggregate audit event (e.g. `{"count": n}`). **DO** emit one audit event per record actually disclosed, each naming the key/resource but never the value — an aggregate count can't answer "which secrets did this admin reveal." Emit the event only after a successful decrypt, so a skipped/corrupt row never gets a phantom entry.
- **Known non-fix**: `audit.Record`/`audit.RecordRequest` (`internal/audit/audit.go`) are deliberately fire-and-forget — they log-and-continue on a persistence failure rather than returning an error, and every call site across the codebase (~7 files) assumes this. A reasonable-sounding review ask is "fail the request if the audit write fails, don't return the secret." Don't do this piecemeal: it would make exactly one or two routes behave differently from every other audited route in the codebase for an inconsistent, partial guarantee. If audit-write-failure-should-block-response ever becomes a real requirement, it needs a deliberate signature change to `audit.Record`/`RecordRequest` plus updating every caller, not a one-off in a single handler.
- **Test naming nitpick to reject**: `TestXxx_ScenarioName` (e.g. `TestRevealEnvVar_AdminGetsPlaintext`) already satisfies the CamelCase rule above — the rule bans `Test_foo_bar` (lowercase-with-underscores after `Test`), not an underscore-separated scenario suffix on an otherwise CamelCase name. This suffix style is the established convention throughout `internal/routes/*_test.go` (see `TestBulkUpsertEnvVars_ReplaceMode`, `TestCopyEnvVars_SkipsWhenOverwriteFalse`) — don't rename it back to `TestRevealEnvVarAdminGetsPlaintext` on review feedback that misreads the rule.
- **DON'T** add a new `_test.go` file to `internal/routes/` with one `TestXxx` function (and one `newSetupTestApp`/`envVarTestApp`-style bootstrap) per scenario. **DO** group same-route scenarios into one outer `TestXxx` with `t.Run` subtests sharing a single app/repo/mux bootstrap, each subtest creating its own stack/row so state can't leak between cases (see `TestRevealEnvVar`/`TestRevealStackEnvVars` in `env_var_reveal_routes_test.go`, and `compose_create_route_test.go` from commit `27dcd087`). Each PocketBase test-app bootstrap costs about a second, `internal/routes` already sits close to `go test`'s 10-minute per-package timeout in CI (which is slower than a typical dev machine), and this exact mistake — adding ~12 one-bootstrap-per-case tests — has caused a real CI timeout twice already.

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
