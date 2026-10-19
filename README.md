<p align="center">
  <img src=".github/assets/logo-readme.png" alt="wireops" width="600">
</p>

[![Latest Release](https://img.shields.io/github/v/release/wireops/wireops?sort=semver)](https://github.com/wireops/wireops/releases/latest)
[![Server CI](https://github.com/wireops/wireops/actions/workflows/server-ci.yml/badge.svg)](https://github.com/wireops/wireops/actions/workflows/server-ci.yml)
[![Worker CI](https://github.com/wireops/wireops/actions/workflows/worker-ci.yml/badge.svg)](https://github.com/wireops/wireops/actions/workflows/worker-ci.yml)
[![govulncheck](https://img.shields.io/badge/govulncheck-passing-brightgreen.svg)](.govulncheck-allowlist.txt)
[![Codacy Grade](https://app.codacy.com/project/badge/Grade/cdc7bea4ca1e44f780110e784d34938a)](https://app.codacy.com/gh/wireops/wireops/dashboard)
[![Codacy Coverage](https://app.codacy.com/project/badge/Coverage/cdc7bea4ca1e44f780110e784d34938a)](https://app.codacy.com/gh/wireops/wireops/dashboard)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPLv3-blue.svg)](LICENSE)

**Push to Git. wireops ships it.** A self-hosted GitOps controller that watches your repos and rolls out `docker compose` changes across every host you own — no Kubernetes, no manifest sprawl, no SSH-and-pray deploys. Think Flux/ArgoCD, but for plain Compose stacks.

📚 **Docs**: [wireops.dev](https://wireops.dev/docs/) · [Português (Brasil)](https://wireops.dev/pt-BR/docs/) — install, architecture, API reference, operations, security, integrations, troubleshooting. Vulnerabilities go to [SECURITY.md](SECURITY.md), not a public issue.

## What it does

**GitOps**

- Polls each repo (interval per stack, or a push webhook) and deploys on every new commit
- One compose file *is* the stack: an `x-wireops` block carries name, worker tags, timeouts and sync interval, and `docker compose` still runs the file as-is
- Rollback to any previous commit; every deploy is kept as a numbered revision you can read and diff
- Per-phase deploy timeline (fetch → lint → render → secrets → policy → dispatch → compose up → post-check → notify) instead of one opaque log blob
- Adopt what's already running: discover unmanaged Compose projects on a worker and import them; migrate a stack to another repository with a preview of what changes first
- Cron-scheduled one-shot Docker jobs (`job.yaml`), with run history, manual runs and cancellation

**Guardrails**

- Compose lint before anything ships — unpinned tags, missing restart policy or healthcheck, plaintext secrets, ports on `0.0.0.0`, and worker-policy breaches, each mapped back to the line in your file
- Per-worker deploy policy: allowlists for images/volumes/networks/capabilities, plus blocks for privileged, host network/PID/IPC, docker socket, `:latest`
- Four roles (`monitoring` · `viewer` · `operator` · `admin`) over capability-checked routes, SSO group → role mapping, and an audit log of every change
- Secrets encrypted at rest (AES-GCM), or kept in HashiCorp Vault, Infisical, or SOPS+age files committed next to your compose file

**Day-2**

- Live container state, logs, stats, and a per-stack dependency graph of services, networks and volumes
- Audited web terminal into a container, RBAC-gated, with asciicast transcripts and idle timeouts
- Backups from the UI — scheduled, retained, optionally mirrored to S3-compatible storage with client-side encryption — plus restore and upload
- Render-time overrides (image/ports/networks/scale) for one-off validation, without a git commit
- Private registry credentials scoped per stack, never written to the worker's `~/.docker/config.json`
- Prometheus `/metrics`, webhook/Discord/Slack/ntfy notifications, and a read-only [MCP server](https://wireops.dev/docs/reference/mcp-server/) so an AI agent can inspect stacks and scaffold new ones

**Identity**

- SSO login via any OIDC provider; native OAuth connect for GitHub and GitLab (including self-hosted)
- Service accounts with scoped API keys for metrics scraping and automation

## How it works

Two binaries from one Go module. The **server** (PocketBase + an embedded Nuxt SPA) does reconciliation, scheduling and state — it never touches Docker. The **workers** connect *out* to the server over a token-authenticated WebSocket and are the only thing that runs `docker compose` on a host. Add a host by starting a worker with a token; nothing needs to reach into it.

See the [architecture guide](https://wireops.dev/docs/reference/architecture/) for the full picture.

## Screenshots

*Click any screenshot to zoom.*

<p align="center">
  <a href=".github/assets/screenshots/stacks-overview.png"><img src=".github/assets/screenshots/stacks-overview.png" alt="Stacks overview" width="800"></a><br>
  <sub><b>Stacks</b> — every Compose deployment, sync/deploy status, and assigned worker</sub>
</p>

<p align="center">
  <a href=".github/assets/screenshots/dependency-graph.png"><img src=".github/assets/screenshots/dependency-graph.png" alt="Stack dependency graph" width="420"></a>
  &nbsp;
  <a href=".github/assets/screenshots/jobs.png"><img src=".github/assets/screenshots/jobs.png" alt="Cron jobs" width="420"></a><br>
  <sub><b>Dependency graph</b> — services, networks and volumes per stack · <b>Jobs</b> — cron-scheduled one-shot containers with run history</sub>
</p>

<p align="center">
  <a href=".github/assets/screenshots/integrations.png"><img src=".github/assets/screenshots/integrations.png" alt="Integrations" width="420"></a>
  &nbsp;
  <a href=".github/assets/screenshots/mobile-stack-detail.png"><img src=".github/assets/screenshots/mobile-stack-detail.png" alt="Mobile stack detail" width="180"></a><br>
  <sub><b>Integrations</b> — Traefik, Caddy, Dozzle, Vault, Infisical, SOPS, S3, and more · <b>Mobile</b> — manage stacks and secrets from your phone</sub>
</p>

## Requirements

- A Linux host on `amd64` or `arm64`
- Docker Engine `25.0+` and Docker Compose `v2.24.1+` (`docker compose`)
- Worker access to the Docker socket or a configured Docker endpoint
- Network access from every worker to the server — TLS or a private network for remote workers

Keep the server and every worker on the exact same release; read the [compatibility policy](https://wireops.dev/docs/operations/compatibility/) before upgrading either.

## Quick start

The server comes up first, then one or more workers register against it. `example/docker-compose.yml` has both.

**1. Start the server.** `SECRET_KEY` encrypts every secret wireops stores — git passwords, SSH keys, integration tokens — so treat it like a production credential:

```bash
cp example/.env.example example/.env
# Set SECRET_KEY (openssl rand -hex 32), BOOTSTRAP_TOKEN, WORKER_TAGS,
# and DOCKER_GID on Linux. Set APP_URL if this isn't localhost.

cd example
docker compose up -d wireops
```

**2. Create the first admin.** There are no default credentials. Open `http://localhost:8090/setup`, enter your `BOOTSTRAP_TOKEN`, create the account. The route closes itself once an admin exists — keeping or dropping the token afterwards changes nothing, but dropping it is tidier.

**3. Issue a worker token.** **Workers → Add Worker** in the UI, copy the token.

**4. Start a worker.**

```bash
# still in example/
WORKER_TOKEN=paste-the-token-here WORKER_TAGS=prod,eu-west-1 docker compose up -d wireops-worker
```

It connects out, registers, and flips to `ACTIVE` in **Workers** within a few seconds. `WORKER_TAGS` is what a worker is *for* (`prod`, `eu-west-1`) — required, comma-separated, set on the worker itself. Stacks and jobs can require tags, so only matching workers show up as targets.

> **Linux permissions:** the containers run as UID/GID `1000`. Create `example/data` and make it writable by that identity before the first start, and set `DOCKER_GID` to the Docker socket's numeric group (`stat -c '%g' /var/run/docker.sock`). `example/.env.example` has the commands. Linux is the supported production environment.

The example pins `WIREOPS_VERSION` rather than tracking `latest`. Port `8443` is plain HTTP/WebSocket while `TLS_ENABLED=false` — remote workers should use native TLS or a private network. Read the [production guidance](https://wireops.dev/docs/operations/production/) before exposing an instance.

## Defining a stack

A stack is a compose file in a Git repository, plus a top-level `x-wireops` block telling wireops how to deploy it. `x-*` is a [compose extension field](https://docs.docker.com/reference/compose-file/extension/), so `docker compose up` still runs the file unchanged:

```yaml
x-wireops:
  version: wireops.v1
  name: api
  group: platform           # optional, for grouping in the UI
  timeout: 5m               # deploy timeout
  sync:
    interval: 2m            # how often this repo is polled for this stack
  worker:
    tags: [prod, amd64]     # which workers may run it
  compose:
    remove_orphans: true
    force_pull: false
  jobs:
    wait_running: true      # wait for related jobs before deploying

services:
  api:
    image: ghcr.io/acme/api:1.4.2
    restart: unless-stopped
```

Point a stack at the file in the UI (**Stacks → New**) and wireops re-reads the block from git on every sync — those fields aren't editable through the API, so git stays the source of truth. The older two-file layout (a separate `wireops.yaml`) still works, but `x-wireops` is the path for anything new.

Environment variables live in wireops — per stack, per job, or shared globally — can be marked secret, and support bulk edit and copy-from-another-stack. Config files committed to the repo mount into services via a `dev.wireops.config.<name>` annotation, checksum-tracked so drift shows up.

### Scheduled jobs

A `job.yaml` in a repo defines a one-shot container on a cron schedule. The file is the single source of truth and the `resources` block is mandatory:

```yaml
title: "Database Backup"
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

## Secrets

Four ways to get a value into a container, pick per variable:

| Backend | Where the value lives |
|---|---|
| `internal` | wireops' own DB, AES-GCM encrypted with `SECRET_KEY` |
| `vault` | HashiCorp Vault KV v2, referenced as `<mount>/data/<path>#<field>`, fetched at deploy time |
| `infisical` | Infisical, referenced as `<project>/<env>/<path>#<NAME>`, fetched at deploy time |
| SOPS + age | A `secrets.yaml` committed next to your compose file, decrypted during sync with the repo's auto-generated age key |

Nothing plaintext is ever persisted, a deploy is blocked up front if a referenced backend is disabled, and reveal actions are audited. Details: [Secrets](https://wireops.dev/docs/reference/environment-variables/).

## Day-2 operations

Short version of what's in the UI — the [docs](https://wireops.dev/docs/) have the full story on each.

- **Rollback & revisions** — redeploy any earlier commit; read or diff the exact rendered compose file of any past deploy.
- **Render overrides** — swap a service's `image`, `ports`, `networks` or `scale` without a commit, for one-off validation. Stored on the stack, applied at render time, still subject to every policy check. Gated by the `allow_render_overrides` worker-policy flag (off by default). `scale: 0` stops a service; scale-only changes adjust replicas without recreating containers.
- **Registry credentials** — Docker Hub, GHCR, Harbor/Nexus/`registry:2`, or a GCP service-account key. Assign one to a stack and the worker authenticates that deploy's pulls only. Reaching a registry on a private network means opting its range into `ALLOWED_PRIVATE_IP_RANGES` — the same SSRF guard that covers git host-key scanning.
- **Backups** — create, schedule, retain, download and restore from the UI, no PocketBase superuser needed. Enable the S3 integration and every new backup is also mirrored off-host, encrypted client-side (`SECRET_KEY`-derived, or an AWS KMS-wrapped data key). Mirroring replicates, never moves. The archive covers the database — not the git checkouts and rendered revisions under `DATA_DIR`. See [Disaster Recovery](https://wireops.dev/docs/operations/disaster-recovery/).
- **Web terminal** — a shell in a running container, behind the `use_terminal` capability, with per-worker session limits, a 15-minute idle timeout, and an asciicast transcript kept 30 days. Transcripts capture everything typed and shown, secrets included.
- **Custom thumbnails** — add `customization.image.slug=nginx` to a service's labels (or `annotations`/`deploy.labels`) and the UI shows that icon from the [selfh.st/icons](https://selfh.st/icons/) catalog.

## Integrations

Plugins register themselves in `internal/integrations/` and are all configured from the same Settings screen:

| Kind | Plugins |
|---|---|
| Container actions | Traefik, Caddy, Nginx Proxy Manager, Dozzle — add "Open"/"Logs" shortcuts to the container list |
| Notifications | Webhook, Discord, Slack, ntfy — fire on sync events |
| Secret backends | Vault, Infisical, SOPS |
| Storage | S3-compatible, for off-host backup mirroring |
| Source control | GitHub, GitLab — OAuth connect, enabled by setting the provider's client ID/secret |

Plugin interface, config fields and label examples: [Integrations](https://wireops.dev/docs/integrations/).

## Configuration

`SECRET_KEY` is required. `BOOTSTRAP_TOKEN` is required only until the first admin exists. Everything else has a default.

Full reference — server and worker variables, SMTP, OIDC/SSO, Vault/Infisical, SOPS+age, TLS: [Environment Variables](https://wireops.dev/docs/reference/environment-variables/). Also: [Observability](https://wireops.dev/docs/reference/observability/) (metrics, Prometheus/Grafana scrape config) · [MCP Server](https://wireops.dev/docs/reference/mcp-server/) · [Disaster Recovery](https://wireops.dev/docs/operations/disaster-recovery/).

## Production

The quick start is there to prove the install. Before you run anything you care about: pin the image version, enable TLS for remote workers, configure off-host backups, keep `SECRET_KEY` somewhere separate, and test one restore. Then follow the [production checklist](https://wireops.dev/docs/operations/production/), [compatibility policy](https://wireops.dev/docs/operations/compatibility/) and [upgrade guide](https://wireops.dev/docs/operations/upgrading/).

## Development

Go + PocketBase on the backend, Nuxt 4 + Vue 3 + Nuxt UI on the frontend, SQLite for storage.

```bash
make dev                  # backend :8090 (admin at /_/) + frontend :3000
START_WORKER=true WORKER_TOKEN=... make dev    # ...and a local worker

go test ./...             # backend
cd frontend && npm run test && npm run lint

make all                  # production build: SPA into pb_public/, then the binary
```

`AGENTS.md` is the deep reference — data model, every custom endpoint, business flows, conventions.

## Scope

Built for developers, homelabs and self-hosters running plain `docker compose` across one or many hosts and repos: GitOps sync and deploy, without manifest sprawl or control-plane complexity.

It is **not** a Kubernetes or Swarm alternative, and not a competitor to Flux/ArgoCD. If you need multi-node autoscaling, HA or compliance-grade orchestration, use Kubernetes. No OCI-artifact source, Swarm/multi-node support, or canary/preview deploys — those are strategic backlog with no ETA.

## License

AGPLv3 — see [LICENSE](LICENSE). wireops is free/open-source software. Forks and derivative works must also be AGPLv3 (or compatible) and must credit **wireops** ([github.com/wireops/wireops](https://github.com/wireops/wireops)) as the original project — see [NOTICE](NOTICE).

## Contributing

Issues and PRs welcome. Security reports follow [SECURITY.md](SECURITY.md) instead of a public issue. User-visible changes go in the `Unreleased` section of [CHANGELOG.md](CHANGELOG.md).
