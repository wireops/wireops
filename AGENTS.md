# wireops — Agent Context

Self-hosted GitOps platform for managing Docker Compose stacks and scheduled Docker-based jobs. It watches Git repositories for changes and deploys updates on remote hosts through token-authenticated WebSocket workers.

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
│   ├── integrations/             # Plugin registry (Traefik, Dozzle, …)
│   ├── job/parser.go             # job.yaml parsing & validation
│   ├── jobscheduler/scheduler.go # Cron scheduler for Docker-based jobs
│   ├── notify/                   # Outbound notifications (webhook / ntfy)
│   ├── protocol/messages.go      # WebSocket message types (shared)
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
| `repositories` | `name`, `git_url`, `branch`, `status`, `last_commit_sha`, `platform` |
| `repository_keys` | `repository`, `auth_type` (none/ssh_key/basic), `ssh_private_key`*, `git_password`* |
| `stacks` | `name`, `repository`, `compose_path`, `auto_sync`, `status`, `worker`, `current_version` |
| `stack_env_vars` | `stack`, `key`, `value`*, `secret` |
| `stack_services` | `stack`, `service_name`, `container_name`, `status` |
| `stack_revisions` | `stack`, `version` — numbered snapshots of rendered compose YAML |
| `stack_pending_reconciles` | `stack`, `trigger`, `commit_sha` — queue for offline worker reconnect |
| `sync_logs` | `stack`, `trigger`, `status`, `output`, `duration_ms` |
| `workers` | `hostname`, `fingerprint`, `status` (ACTIVE/REVOKED), `health_history` |
| `scheduled_jobs` | `repository`, `job_file`, `enabled`, `status` |
| `job_runs` | `job`, `worker`, `status`, `output`, `expires_at` (30-day TTL) |
| `integrations` | `slug`, `enabled`, `config` (JSON) |

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
3. `sync/renderer.go` reads the compose YAML, injects `dev.wireops.*` labels, and writes a versioned revision file to `DATA_DIR/stacks/<id>/v<n>.yml`.
4. The server base64-encodes the compose file, sends a `DeployCommand` over WebSocket to the worker assigned to the stack, and waits up to 5 minutes for `CommandResult`.
5. The worker decodes the compose file and executes `docker compose up`.
6. Persists a `sync_logs` entry, updates stack status, fires webhook/ntfy notification.

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
| `GET` | `/stacks/{id}/stream` | SSE log stream |
| `GET` | `/stacks/{id}/container/{cid}/stats` | CPU/mem stats |
| `GET` | `/stacks/{id}/container/{cid}/logs` | Container logs |
| `POST` | `/stacks/{id}/container/stop` | Stop a container |
| `POST` | `/stacks/{id}/container/restart` | Restart a container |
| `GET` | `/stacks/import/discover` | Discover unmanaged Compose projects |
| `POST` | `/stacks/import` | Import a local Compose stack |

### Repositories
| Method | Path | Description |
|---|---|---|
| `GET` | `/repositories/{id}/commits` | Last 5 commits |
| `GET` | `/repositories/{id}/files` | List `.yml`/`.yaml` files |
| `POST` | `/credentials/test` | Test git credentials |
| `POST` | `/credentials/keyscan` | SSH host key scan |

### Workers (superuser only)
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/custom/workers` | List all workers (including pending tokens) |
| `POST` | `/api/custom/worker/tokens` | Generate worker token |
| `POST` | `/api/custom/workers/{id}/revoke` | Revoke worker or a pending token (using `pending:{tokenRecordId}`) |

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

### Worker
| Variable | Description |
|---|---|
| `SERVER_URL` | HTTPS URL of the wireops server |
| `WORKER_TOKEN` | Worker authorization token |
| `WORKER_TAGS` | Comma-separated tags (used for job routing) |

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
- **Traefik** (`traefik`) — reads router labels and builds clickable "Open" links.
- **Dozzle** (`dozzle`) — adds "Logs" links pointing to a self-hosted Dozzle instance.

---

## Notifications

Configured via the `stack_sync_events` collection (singleton). Supported providers:
- **Webhook** — HTTP POST with HMAC-SHA256 signature and custom headers.
- **Ntfy** — push notifications via ntfy.sh (configurable topic, user, template).

Events: `sync.started`, `sync.done`, `sync.error`, `sync.test`.

---

## Coding Conventions

- All custom API handlers are in `internal/routes/`; each file groups a domain.
- Secrets are always encrypted before persistence via `internal/crypto/encrypt.go`; never store plaintext.
- WebSocket protocol message types are defined in `internal/protocol/messages.go` and shared by both server and worker.
- PocketBase schema changes are Go migration files in `pb_migrations/`; always add a new numbered file, never modify existing ones.
- Path safety for user-supplied file paths must go through `internal/safepath/`.
- Frontend pages live in `frontend/app/pages/` (Nuxt file-based routing); shared composables in `frontend/app/composables/`.
- Test function names must be in CamelCase.

## Testing & Coverage

- Backend: `go test -coverprofile=coverage.out $(go list ./... | grep -v '^github.com/wireops/wireops/worker\(/\|$\)')` (excludes `worker/...`, run separately). Current baseline: ~29% statement coverage overall — uneven across packages (e.g. `internal/config`, `internal/webhook`, `internal/setup` are near/at 100%; `internal/routes`, `internal/sync`, `internal/compose` are thin).
- Frontend: `npm run test:coverage` (vitest + `@vitest/coverage-v8`, reports `text` + `lcov` to `frontend/coverage/`). Current baseline: ~62% statement coverage.
- Both are uploaded to Codacy via `.github/workflows/quality-codacy.yml` (`continue-on-error: true` — informational, not a merge gate).
- **Minimum targets** (hobby/side-project pace, not enforced by CI — treat as a floor when touching a package, not a blanket requirement to backfill):
  - Backend: **25%** overall statement coverage. New/changed `internal/*` packages with non-trivial logic (parsing, reconciliation, auth/rbac, encryption) should carry tests; thin glue code (routes wiring, migrations) is exempt.
  - Frontend: **50%** overall statement coverage. Prioritize composables and utils (`frontend/app/composables/`, `frontend/app/utils/`) over component markup.
- These are floors, not aspirational targets — raise them only once coverage comfortably clears them for a few months.
