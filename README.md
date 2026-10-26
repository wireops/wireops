<p align="center">
  <img src=".github/assets/logo-readme.png" alt="wireops" width="600">
</p>

[![Latest Release](https://img.shields.io/github/v/release/wireops/wireops?sort=semver)](https://github.com/wireops/wireops/releases/latest)
[![Server CI](https://github.com/wireops/wireops/actions/workflows/server-ci.yml/badge.svg)](https://github.com/wireops/wireops/actions/workflows/server-ci.yml)
[![Worker CI](https://github.com/wireops/wireops/actions/workflows/worker-ci.yml/badge.svg)](https://github.com/wireops/wireops/actions/workflows/worker-ci.yml)
[![MCP CI](https://github.com/wireops/wireops/actions/workflows/mcp-ci.yml/badge.svg)](https://github.com/wireops/wireops/actions/workflows/mcp-ci.yml)
[![Known Vulnerabilities](https://snyk.io/test/github/wireops/wireops/badge.svg)](https://snyk.io/test/github/wireops/wireops)
[![govulncheck](https://img.shields.io/badge/govulncheck-passing-brightgreen.svg)](.govulncheck-allowlist.txt)
[![Go Version](https://img.shields.io/github/go-mod/go-version/wireops/wireops)](go.mod)
[![Node Version](https://img.shields.io/badge/node-26-brightgreen.svg)](.github/workflows/server-ci.yml)
[![Nuxt](https://img.shields.io/badge/Nuxt-4.6-00DC82.svg?logo=nuxtdotjs&logoColor=white)](frontend/package.json)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPLv3-blue.svg)](LICENSE)
[![Codacy Grade](https://app.codacy.com/project/badge/Grade/cdc7bea4ca1e44f780110e784d34938a)](https://app.codacy.com/gh/wireops/wireops/dashboard)
[![Codacy Coverage](https://app.codacy.com/project/badge/Coverage/cdc7bea4ca1e44f780110e784d34938a)](https://app.codacy.com/gh/wireops/wireops/dashboard)

**Container management for AI.** wireops is a self-hosted control plane that lets an AI agent run containers across every host you own — inspect, deploy, roll back, run jobs, exec into a container — through 66 MCP tools, under the same RBAC, policy engine and audit log a human operator gets. Git stays the source of truth underneath: the agent proposes, the repo records, the controller reconciles. No Kubernetes, no manifest sprawl, no handing an agent your Docker socket.

📚 **Documentation**: [wireops.dev](https://wireops.dev/docs/) · [Português (Brasil)](https://wireops.dev/pt-BR/docs/) — install, architecture, API reference, operations, security, integrations, troubleshooting. Security reports go to [SECURITY.md](SECURITY.md).

## Why an agent can be trusted with this

Pointing an agent at a host usually means giving it the Docker socket and hoping. wireops puts the same guardrails between an agent and your containers that it puts between a junior operator and production:

- **Every agent brings its own key.** The MCP server holds no credential of its own — each client supplies a `service_accounts` API key per connection, pass-through, never persisted. Revoke one agent without touching the others.
- **Authorization is the server's, not the agent's.** Every call lands on the same REST route the UI calls, so `internal/rbac` decides it. A service account caps at `viewer`/`operator`, which is a hard ceiling: an agent cannot change settings, read the audit log, or escalate itself, no matter how it is prompted.
- **The audit log keeps per-agent granularity.** Because the key isn't shared, every deploy, rollback and exec is attributable to the agent that made it.
- **The tools that can't be undone make you type the name.** Deleting a stack, repository or job, revoking a worker, or stopping a container takes a `confirm` field that the server matches against the target's own name, resolved server-side — a wrong guess fails before any write leaves the process. Some tools gate only their destructive branch (`force_redeploy_stack` when it recreates volumes, the env var writes in `replace` mode), and `delete_job_run` takes the literal `delete`. The reversible writes don't pay that cost.
- **Tools are annotated, so your client can decide.** Each one carries `readOnly` / `additive` / `destructive` hints, which is what lets a client run reads unattended and hold the rest for a human. The annotation is advice to the client, not a server-side gate — the `confirm` guard above is the enforced one, and it covers a deliberately narrower set.
- **Deploys report an observed outcome instead of leaving the agent to guess.** `deploy_stack`, `force_redeploy_stack` and `rollback_stack` can wait on the reconcile and return its terminal status with the deploy log. The wait subscribes before triggering, so it can't miss the result — but it cannot *prove* the reconcile it observed is the one it triggered, since wireops skips a trigger for a stack that is already syncing. When the status belongs to a sync log that already existed, the tool says so in a `_notice` rather than passing it off as this call's result.

Three tool groups (`MCP_TOOLS`), so you choose the blast radius: `read` for an agent that only answers questions, `write` to let it deploy, `exec` to let it into a container.

Add it as a service alongside the server, so `SERVER_URL` resolves over the Compose project's own network:

```yaml
  wireops-mcp:
    image: ghcr.io/wireops/mcp:${WIREOPS_VERSION:-1.0.0}
    restart: unless-stopped
    depends_on:
      wireops:
        condition: service_healthy
    environment:
      - SERVER_URL=http://wireops:8090
      - MCP_TOOLS=read
    ports:
      - "8091:8091"
```

Running it standalone instead means giving it a route to the server yourself: Docker's default bridge does not resolve container names, so a bare `docker run` needs `--network <the server's compose network>` — or a `SERVER_URL` the container can actually reach.

Streamable HTTP at `POST/GET /mcp`; point your client at it and send the API key as `X-Wireops-Api-Key` (or `Authorization: Bearer`). Full tool table, resources and prompts: [MCP Server](https://wireops.dev/docs/reference/mcp-server/).

## Features

**For agents**

- 🤖 66 MCP tools over the full lifecycle — read stacks, scaffold new ones, deploy, roll back, pause, run jobs, exec into containers
- 🔑 Per-agent scoped API keys, pass-through auth, revocable individually, capped at `operator`
- 🧾 Every agent action through the same RBAC, worker policy and audit log as a human's
- 📡 Subscribable live-log resource (`wireops://stacks/{id}/logs/live`), plus compose and job-file resources
- 📐 A conventions resource so a scaffolded stack comes out matching your house style
- 🧠 Names accepted wherever an id is — ambiguity is an error naming the candidates, never a guess
- 🔍 `lint_compose` and `preview_stack_migration` are read-only, so an agent can check its own work before deploying

**For the humans who own the hosts**

- 🐳 Docker Compose stack management
- 🔄 Automatic synchronization from Git repositories
- 📄 Single-file stacks — one compose file with an `x-wireops` block, still runnable by `docker compose`
- 🔍 Compose linting before deploy, with findings mapped to the line in your file
- 📊 Real-time container monitoring, logs and stats
- ⏱️ Per-phase deploy timeline — fetch, lint, render, secrets, policy, dispatch, compose up, post-check
- 🕸️ Per-stack dependency graph of services, networks and volumes
- 🔐 Share secrets to your Stack/Job with SOPS+age, HashiCorp Vault, Infisical integrations
- 🗂️ Config files committed to git, mounted into services and tracked by checksum
- 🛡️ Strong Policy and RBAC system, with audit logging
- 🔑 SSO login via any OIDC provider, plus service accounts with scoped API keys
- 🐙 Native GitHub and GitLab OAuth connect, including self-hosted GitLab
- 🌐 Webhook, Discord, Slack, and ntfy notifications
- 📈 Prometheus `/metrics` for the server and every worker
- 🚀 Force redeploy with recreate options
- 🗓️ Cron-scheduled one-shot Docker jobs
- 🔄 Rollback to previous commits, with every deploy kept as a readable revision
- 📦 Private registry credentials, selected automatically by each image's exact registry host and port
- 💾 Backups with schedule, retention and restore — optionally mirrored to S3, encrypted client-side
- 📥 Import Compose projects already running on a host, or migrate a stack to another repository
- 💻 Audited web terminal, RBAC-gated
- 🎛️ Optional render-time stack overrides (image/ports/networks/scale) for one-off validation testing, without a git commit

## Screenshots

*Click any screenshot to zoom.*

<p align="center">
  <a href=".github/assets/screenshots/stacks-overview.png"><img src=".github/assets/screenshots/stacks-overview.png" alt="Stacks overview" width="800"></a><br>
  <sub><b>Stacks</b> — every Compose deployment, sync/deploy status, and assigned worker</sub>
</p>

<p align="center">
  <a href=".github/assets/screenshots/dependency-graph.png"><img src=".github/assets/screenshots/dependency-graph.png" alt="Stack dependency graph" width="400"></a>
  &nbsp;
  <a href=".github/assets/screenshots/jobs.png"><img src=".github/assets/screenshots/jobs.png" alt="Cron jobs" width="400"></a><br>
  <sub><b>Dependency graph</b> — services, networks and volumes per stack · <b>Jobs</b> — cron-scheduled containers with run history</sub>
</p>

<p align="center">
  <a href=".github/assets/screenshots/integrations.png"><img src=".github/assets/screenshots/integrations.png" alt="Integrations" width="400"></a>
  &nbsp;
  <a href=".github/assets/screenshots/mobile-stack-detail.png"><img src=".github/assets/screenshots/mobile-stack-detail.png" alt="Mobile stack detail" width="180"></a><br>
  <sub><b>Integrations</b> — Traefik, Caddy, Dozzle, Vault, Infisical, SOPS, S3, and more · <b>Mobile</b> — manage stacks from your phone</sub>
</p>

## How it works

Three deployables. The **server** (Go + PocketBase, with the Nuxt SPA embedded) handles reconciliation, scheduling and state — it never touches Docker. The **workers** connect *out* to it over a token-authenticated WebSocket and are the only thing running `docker compose` on a host. Adding a host means starting a worker with a token; nothing reaches into it. The **MCP server** is a separate process in front of the same REST API — it is how agents get in, and it holds no privilege the API key doesn't carry.

That shape is what makes an agent safe to connect: the agent talks to an API, the API decides, and a worker — not the agent — is the only thing with Docker access. See the [architecture guide](https://wireops.dev/docs/reference/architecture/).

## Requirements

- A Linux host on `amd64` or `arm64`.
- Docker Engine `25.0+` and Docker Compose `v2.24.1+` (`docker compose`).
- Worker access to the Docker socket or configured Docker endpoint.
- Network access from every worker to the server; use TLS or a private network for remote workers.

Keep the server and every worker on the exact same release — see the [compatibility policy](https://wireops.dev/docs/operations/compatibility/) before upgrading either.

## Quick Start

Server first, then one or more workers that register against it. `example/docker-compose.yml` has both.

**1. Start the server.** `SECRET_KEY` encrypts every secret wireops stores — git passwords, SSH keys, integration tokens — so treat it like a production credential:

```bash
cp example/.env.example example/.env
# Set SECRET_KEY (openssl rand -hex 32), BOOTSTRAP_TOKEN, WORKER_TAGS,
# and DOCKER_GID on Linux. Set APP_URL if this isn't localhost.

cd example
docker compose up -d wireops
```

**2. Create the first administrator.** There are no default credentials. Open `http://localhost:8090/setup`, enter your `BOOTSTRAP_TOKEN` and create the account. The route closes itself once an admin exists; keeping or dropping the token afterwards changes nothing, but dropping it is tidier.

**3. Issue a worker token.** **Workers → Add Worker** in the UI, then copy the token.

**4. Start a worker.**

```bash
# still in example/
WORKER_TOKEN=paste-the-token-here WORKER_TAGS=prod,eu-west-1 docker compose up -d wireops-worker
```

It connects out, registers, and flips to `ACTIVE` in **Workers** within a few seconds. `WORKER_TAGS` says what a worker is *for* (`prod`, `eu-west-1`) — required, comma-separated, set on the worker itself. Stacks and jobs can require tags, so only matching workers show up as targets.

> **Linux permissions:** the containers run as UID/GID `1000`. Create `example/data` writable by that identity before the first start, and set `DOCKER_GID` to the Docker socket's numeric group (`stat -c '%g' /var/run/docker.sock`). `example/.env.example` has the commands. Linux is the supported production environment.

The example pins `WIREOPS_VERSION` instead of tracking `latest`. Port `8443` is plain HTTP/WebSocket while `TLS_ENABLED=false` — remote workers should use native TLS or a private network. Read the [production guidance](https://wireops.dev/docs/operations/production/) before exposing an instance.

## Defining a stack

A stack is a compose file in a Git repository plus a top-level `x-wireops` block telling wireops how to deploy it. `x-*` is a [compose extension field](https://docs.docker.com/reference/compose-file/extension/), so `docker compose up` still runs the file unchanged:

```yaml
x-wireops:
  version: wireops.v1
  name: api
  group: platform         # optional, groups stacks in the UI
  timeout: 5m             # deploy timeout
  sync:
    interval: 2m          # how often this stack's repo is polled
  worker:
    tags: [prod, amd64]   # which workers may run it
  compose:
    remove_orphans: true
  jobs:
    wait_running: true    # wait for related jobs before deploying

services:
  api:
    image: ghcr.io/acme/api:1.4.2
    restart: unless-stopped
```

Point a stack at the file in **Stacks → New** and wireops re-reads the block from git on every sync — those fields aren't editable through the API, so git stays the source of truth. The older two-file layout (a separate `wireops.yaml`) still works, but `x-wireops` is the path for anything new.

Environment variables live in wireops — per stack, per job, or shared globally — and can be marked secret.

### Scheduled Jobs

A `job.yaml` in a repo defines a one-shot container on a cron schedule. The file is the single source of truth, and the `resources` block is mandatory:

```yaml
name: "Database Backup"
description: "Nightly backup of the postgres database"
cron: "0 2 * * *"
image: "postgres:15-alpine"
command: ["pg_dump", "-h", "db", "-U", "postgres", "mydb"]
tags: ["backup", "prod"]
mode: "once"          # once | once_all
volumes:
  - "/opt/backups:/backups"
network: "prod_network"
resources:
  cpu: "0.5"          # required
  memory: "512m"      # required
  timeout: "15m"      # required
```

## Integrations

Plugins register themselves in `internal/integrations/` and are configured from one Settings screen: **container actions** (Traefik, Caddy, Nginx Proxy Manager, Dozzle) add Open/Logs shortcuts to the container list; **notifications** (Webhook, Discord, Slack, ntfy) fire on sync events; **secret backends** (Vault, Infisical, SOPS), **storage** (S3, for backup mirroring) and **source control** (GitHub, GitLab) back the features above.

Plugin interface, config fields and label examples: [Integrations](https://wireops.dev/docs/integrations/).

## Environment Variables

`SECRET_KEY` is required. `BOOTSTRAP_TOKEN` is required only until the first administrator is created. Everything else has a default.

Full server/worker reference, SMTP, OIDC/SSO, Vault/Infisical, SOPS+age: [Environment Variables](https://wireops.dev/docs/reference/environment-variables/) · [Observability](https://wireops.dev/docs/reference/observability/) · [MCP Server](https://wireops.dev/docs/reference/mcp-server/) · [Disaster Recovery](https://wireops.dev/docs/operations/disaster-recovery/).

## Production

The quick start is there to prove the install. Before running anything you care about: pin the image version, enable TLS for remote workers, configure off-host backups, keep `SECRET_KEY` somewhere separate, and test one restore. Then follow the [production checklist](https://wireops.dev/docs/operations/production/), [compatibility policy](https://wireops.dev/docs/operations/compatibility/) and [upgrade guide](https://wireops.dev/docs/operations/upgrading/).

## Development

```bash
make dev                  # backend :8090 (admin at /_/) + frontend :3000
START_WORKER=true WORKER_TOKEN=... make dev    # ...and a local worker

go test ./...             # backend
cd frontend && npm run test && npm run lint

make all                  # production build: SPA into pb_public/, then the binary
```

`AGENTS.md` is the deep reference — data model, every custom endpoint, business flows, conventions.

## Project Scope

For developers, homelabs and self-hosters who want an AI agent — or a person, or both — managing plain `docker compose` across one or many hosts and repos, with an audit trail and a revocable key per agent. Git stays the record of what is deployed, so an agent's work is reviewable the same way a colleague's is.

**Not** a Kubernetes/Swarm alternative, and not a competitor to Flux/ArgoCD. For multi-node autoscaling, HA or compliance-grade orchestration, prefer Kubernetes. No OCI-artifact source, Swarm/multi-node, or canary/preview deploys yet — strategic backlog, no ETA.

The agent layer is a control plane, not an autonomous operator: wireops gives an agent scoped tools, confirmation gates and an audit log, and it has no opinion about which model you point at it or how much you let it do unattended. That ceiling is yours to set, per key and per tool group.

## License

AGPLv3 — see [LICENSE](LICENSE). wireops is free/open-source software. Forks and derivative works must also be released under AGPLv3 (or a compatible license) and must credit **wireops** ([github.com/wireops/wireops](https://github.com/wireops/wireops)) as the original project — see [NOTICE](NOTICE).

## Contributing

Contributions are welcome — open an issue or PR. Security reports must follow [SECURITY.md](SECURITY.md) instead of a public issue. User-visible changes go in the `Unreleased` section of [CHANGELOG.md](CHANGELOG.md).
