# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> Note: `AGENTS.md` in the repo root contains a more detailed agent-context reference (full data model, API endpoint tables, business flows). Read it for deep dives; this file covers commands and orientation.

## What this is

wireops is a self-hosted GitOps controller for Docker Compose stacks (Flux/ArgoCD-style, but for `docker compose` instead of Kubernetes). It watches Git repos for changes and deploys updates on remote hosts through token-authenticated WebSocket workers. It also runs cron-scheduled one-shot Docker jobs (`job.yaml` in a repo).

Two deployables from one Go module: the **server** (`main.go` → `cmd/serve.go`, embeds PocketBase + a Nuxt SPA) and the **worker** (`worker/main.go`, a lightweight agent that connects out to the server over WebSocket and runs `docker compose` / `docker run` on its host).

## Commands

```bash
# Full dev environment (backend :8090, PocketBase admin at /_/, frontend :3000)
make dev
# Add a worker into the dev loop too:
START_WORKER=true WORKER_TOKEN=... make dev

# Individually
make dev-backend        # go run . serve --http=0.0.0.0:8090
make dev-frontend        # cd frontend && npm run dev

# Build
make build-frontend      # nuxt generate -> copies frontend/.output/public into pb_public/
make build                # go build -o wireops .
make all                  # build-frontend + build (this is how production binaries are made — pb_public/ is embedded)
make clean

# Backend tests (worker package tested separately in CI, but works fine together locally)
go test ./...
go test ./internal/rbac/...              # single package
go test -run TestSpecificName ./internal/notify/...   # single test

# Frontend
cd frontend
npm install
npm run dev
npm run test              # vitest run
npm run lint              # eslint .
npm run generate          # static SPA build -> frontend/.output/public
```

There is no repo-wide linter config for Go (no `.golangci.yml`); CI only runs `go test` + `go build` for the backend. Frontend lint is `eslint` via `@nuxt/eslint`.

Test function names must be CamelCase (not `Test_foo_bar`).

## Architecture

- **Server never touches Docker directly.** All `docker compose` / `docker run` execution happens on remote workers, dispatched over a persistent authenticated WebSocket (`internal/worker/server.go`, port `TLS_WORKER_PORT`/8443). The server's job is reconciliation, scheduling, and state — not execution.
- **PocketBase is the entire backend framework**, not just a database: it provides the SQLite DB, REST CRUD (auto-generated per collection), superuser auth, and realtime SSE. Schema changes are Go migration files under `pb_migrations/` — **always add a new numbered migration file, never edit an existing one.**
- **Frontend is a static SPA**, not SSR. `nuxt generate` output is copied into `pb_public/` and served directly by PocketBase — there's no separate frontend server in production.
- **Custom business logic** lives under `internal/routes/*.go` (grouped by domain: stacks, workers, jobs, users) and is mounted under `/api/custom/*`, alongside PocketBase's auto-CRUD REST for every collection.
- **Secrets** (SSH keys, git passwords, env var values marked secret) are AES-GCM encrypted at rest via `internal/crypto/encrypt.go`, keyed by `SECRET_KEY`. Never persist plaintext secrets.
- **WebSocket protocol types** (`DeployCommand`, `TeardownCommand`, `RunJobCommand`, `CommandResult`, `MsgJobCompleted`, etc.) are defined once in `internal/protocol/messages.go` and shared by both server and worker — keep them in sync when changing the protocol.
- **Path safety**: any user-supplied file path (compose paths, job file paths) must go through `internal/safepath/` to prevent traversal.
- **GitOps sync loop**: `internal/sync/scheduler.go` (polling per stack) → `internal/sync/reconciler.go` (clone/fetch + SHA diff) → `internal/sync/renderer.go` (inject `dev.wireops.*` labels, write versioned revision to `DATA_DIR/stacks/<id>/v<n>.yml`) → dispatch `DeployCommand` to the assigned worker → persist `sync_logs`, update stack status, fire webhook/ntfy notification. If the assigned worker is offline, a `stack_pending_reconciles` row queues the reconcile for replay on reconnect.
- **Integrations** (Traefik, Dozzle, …) are a small plugin registry under `internal/integrations/`, registered via `init()`. Each implements `Slug()/Name()/Category()/ResolveContainerActions(...)` to turn container labels into clickable UI actions — read the interface there before adding a new one.
- Frontend: Nuxt 4 SPA, `@nuxt/ui` v4 (Tailwind + Headless UI), file-based routing in `frontend/app/pages/`, shared composables (`useApi`, `useAuth`, `useRealtime`, …) in `frontend/app/composables/`, PocketBase JS SDK wiring in `frontend/app/plugins/pocketbase.ts`.

For the full data model (collections/fields/relationships), the complete custom API endpoint table, and env var reference, see `AGENTS.md` and `README.md` — don't duplicate that lookup work here.
