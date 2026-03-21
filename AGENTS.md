# wireops — Agent Context

Self-hosted GitOps platform for managing Docker Compose stacks and scheduled Docker-based jobs. It watches Git repositories for changes and deploys updates via `docker compose up`, either locally (embedded worker) or on remote hosts (remote workers over mTLS/WebSocket).

---

## Repository Layout

```
.
├── main.go                       # Entrypoint — delegates to cmd/serve.go
├── cmd/serve.go                  # Server bootstrap & dependency wiring
├── go.mod                        # Go module (go 1.25)
├── worker/                       # Standalone remote-worker binary
│   ├── main.go
│   ├── api/client.go             # mTLS HTTP client (register)
│   ├── executor/runner.go        # Executes deploy / teardown / job commands
│   ├── pki/bootstrap.go          # Worker PKI bootstrap (CSR → signed cert)
│   └── sync/websocket.go         # Persistent WebSocket connection to server
├── internal/
│   ├── worker/                   # Server-side worker management
│   │   ├── server.go             # mTLS WebSocket server on :8443
│   │   └── service.go            # Worker CRUD, seat tokens, health tracking
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
│   ├── pki/service.go            # CA, server cert, worker CSR signing
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
├── pki_data/                     # CA & server TLS certificates
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
| HTTP routing | PocketBase router + Gin (mTLS server only) |
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

Three deployable components:

```
┌──────────────────────────────────────────────────┐
│                wireops Server                    │
│  ┌────────────┐  ┌──────────────┐  ┌──────────┐ │
│  │ PocketBase │  │ Sync         │  │ Job      │ │
│  │ API :8090  │  │ Scheduler    │  │ Scheduler│ │
│  └────────────┘  └──────────────┘  └──────────┘ │
│  ┌────────────────────────────────────────────┐  │
│  │  mTLS WebSocket Server :8443               │  │
│  └────────────────────────────────────────────┘  │
│  ┌────────────────────────────────────────────┐  │
│  │  Embedded Worker (local Docker socket)     │  │
│  └────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────┘
              ↑ mTLS WebSocket ↑
  ┌──────────────────────────────┐
  │   Remote Worker              │
  │   Executes docker compose    │
  │   and docker run (jobs)      │
  └──────────────────────────────┘

  Nuxt SPA (pb_public/) served by PocketBase
  ← REST + PocketBase Realtime (SSE)
```

- The server **never** runs `docker compose` for remote stacks — it dispatches commands over a persistent mTLS WebSocket to workers.
- The **embedded worker** runs on the same host and accesses Docker directly via `/var/run/docker.sock`.
- PocketBase handles auth (superusers collection), realtime subscriptions, and the SQLite database.
- The frontend is statically generated (`nuxt generate`) and served from `pb_public/`.

---

## Data Model

All collections are defined via Go migrations in `pb_migrations/`.

| Collection | Key Fields |
|---|---|
| `repositories` | `name`, `git_url`, `branch`, `status`, `last_commit_sha`, `platform` |
| `repository_keys` | `repository`, `auth_type` (none/ssh_key/basic), `ssh_private_key`*, `git_password`* |
| `stacks` | `name`, `repository`, `compose_path`, `poll_interval`, `auto_sync`, `status`, `worker`, `current_version` |
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
1. `sync/scheduler.go` polls each stack on its `poll_interval` (default 60s).
2. `sync/reconciler.go` runs `git.CloneOrFetch` and compares the latest commit SHA with the stored one.
3. `sync/renderer.go` reads the compose YAML, injects `dev.wireops.*` labels, and writes a versioned revision file to `pb_data/stacks/<id>/v<n>.yml`.
4. **Embedded worker**: calls `compose.RunUp` → `docker compose up` locally.
5. **Remote worker**: base64-encodes the compose file, sends a `DeployCommand` over WebSocket, waits up to 5 minutes for `CommandResult`.
6. Persists a `sync_logs` entry, updates stack status, fires webhook/ntfy notification.

### Worker Bootstrap & Communication
1. Admin generates a one-time seat token via `POST /api/custom/worker/seat`.
2. Worker generates a private key + CSR, calls `POST /api/custom/worker/bootstrap` to receive a signed TLS certificate.
3. Worker connects to `POST /worker/register` (mTLS, :8443), then opens a WebSocket to `/worker/ws`.
4. Server dispatches typed commands (`DeployCommand`, `TeardownCommand`, `RunJobCommand`, etc.) as JSON `Envelope` messages.
5. Worker sends heartbeats every 30s; job completions are pushed as unsolicited `MsgJobCompleted` messages.
6. If a worker is offline when a change is detected, a `stack_pending_reconciles` record is created and replayed on reconnect.

### Scheduled Jobs
1. A `scheduled_jobs` record points to a `job.yaml` file inside a repository.
2. `job.yaml` specifies: `title`, `cron`, `image`, `command`, `tags`, `mode` (once / once_all), `volumes`, `network`.
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
| `GET` | `/workers` | List all workers |
| `POST` | `/worker/seat` | Generate bootstrap seat token |
| `POST` | `/worker/bootstrap` | Worker CSR signing |
| `POST` | `/workers/{id}/revoke` | Revoke worker |

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
| `PORT` | PocketBase HTTP port (default: `8090`) |
| `PB_DATA_DIR` | SQLite data directory (default: `./pb_data`) |
| `REPOS_WORKSPACE` | Git clone workspace (default: `./repos`) |
| `WIREOPS_PKI_DIR` | CA and server TLS certs directory (default: `./pki_data`) |
| `WIREOPS_DISABLE_LOCAL_WORKER` | Disable the embedded worker (default: `false`) |
| `WIREOPS_WORKER_TAGS` | Comma-separated tags for the embedded worker |

### Worker
| Variable | Description |
|---|---|
| `WIREOPS_SERVER` | HTTP URL of the wireops server (for bootstrap) |
| `WIREOPS_MTLS_SERVER` | HTTPS URL for mTLS connections (port 8443) |
| `WIREOPS_BOOTSTRAP_TOKEN` | One-time seat token for initial PKI setup |
| `WIREOPS_WORKER_TAGS` | Comma-separated tags (used for job routing) |

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
