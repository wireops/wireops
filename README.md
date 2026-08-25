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
[![Nuxt](https://img.shields.io/badge/Nuxt-4.5-00DC82.svg?logo=nuxtdotjs&logoColor=white)](frontend/package.json)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPLv3-blue.svg)](LICENSE)
[![Codacy Badge](https://app.codacy.com/project/badge/Grade/cdc7bea4ca1e44f780110e784d34938a)](https://app.codacy.com/gh/wireops/wireops/dashboard?utm_source=gh&utm_medium=referral&utm_content=&utm_campaign=Badge_grade)
[![Codacy Badge](https://app.codacy.com/project/badge/Coverage/cdc7bea4ca1e44f780110e784d34938a)](https://app.codacy.com/gh/wireops/wireops/dashboard?utm_source=gh&utm_medium=referral&utm_content=&utm_campaign=Badge_coverage)

**Push to Git. wireops ships it.** A self-hosted GitOps controller that watches your repos and rolls out `docker compose` changes across every host you own — no Kubernetes, no extra control-plane manifests, no manual SSH-and-pray deploys. Think Flux/ArgoCD, but for plain Compose stacks.

📚 **Full technical docs** (architecture, data model, API reference, env vars, MCP server, disaster recovery, integrations) live in the **[Wiki](https://github.com/wireops/wireops/wiki)**. Operational guides kept with the code: [production](docs/PRODUCTION.md) · [upgrades](docs/UPGRADING.md) · [compatibility](docs/COMPATIBILITY.md) · [disaster recovery](docs/DISASTER_RECOVERY.md) · [troubleshooting](docs/TROUBLESHOOTING.md) · [security policy](SECURITY.md).

## Table of Contents

- [Features](#features)
- [Screenshots](#screenshots)
- [Project Scope](#project-scope)
- [Tech Stack](#tech-stack)
- [Requirements](#requirements)
- [Quick Start](#quick-start)
- [Usage](#usage)
- [Customization](#customization)
- [Environment Variables](#environment-variables)
- [Production](#production)
- [Integrations](#integrations)
- [Development](#development)
- [Known Limitations](#known-limitations)
- [License](#license)
- [Contributing](#contributing)

## Features

- 🐳 Docker Compose stack management
- 🔄 Automatic synchronization from Git repositories
- 📊 Real-time container monitoring
- 🔐 Share secrets to your Stack/Job with SOPS+age, HashiCorp Vault, Infisical integrations
- 🛡️ Strong Policy and RBAC system, with audit logging
- 🔑 SSO login via any OIDC provider
- 🌐 Webhook, Discord, Slack, and ntfy notifications
- 🚀 Force redeploy with recreate options
- 🗓️ Cron-scheduled one-shot Docker jobs
- 🔄 Rollback to previous commits
- 🐙 Native GitHub OAuth connect
- 💻 Audited web terminal, RBAC-gated
- 🎛️ Optional render-time stack overrides (image/ports/networks/scale) for one-off validation testing, without a git commit

## Screenshots

*Click any screenshot to zoom.*

### Desktop

<p align="center">
  <a href=".github/assets/screenshots/stacks-overview.png"><img src=".github/assets/screenshots/stacks-overview.png" alt="Stacks overview" width="800"></a><br>
  <sub><b>Stacks</b> — every Compose deployment, sync/deploy status, and assigned worker</sub>
</p>

<p align="center">
  <a href=".github/assets/screenshots/dependency-graph.png"><img src=".github/assets/screenshots/dependency-graph.png" alt="Stack dependency graph" width="500"></a><br>
  <sub><b>Dependency graph</b> — visualize service, network, and volume dependencies per stack</sub>
</p>

<p align="center">
  <a href=".github/assets/screenshots/jobs.png"><img src=".github/assets/screenshots/jobs.png" alt="Cron jobs" width="500"></a><br>
  <sub><b>Jobs</b> — cron-scheduled one-shot Docker jobs with run history</sub>
</p>

<p align="center">
  <a href=".github/assets/screenshots/integrations.png"><img src=".github/assets/screenshots/integrations.png" alt="Integrations" width="500"></a><br>
  <sub><b>Integrations</b> — Traefik, Caddy, Dozzle, Vault, Infisical, SOPS, and more</sub>
</p>

### Mobile

<p align="center">
  <a href=".github/assets/screenshots/mobile-dashboard.png"><img src=".github/assets/screenshots/mobile-dashboard.png" alt="Mobile dashboard" width="220"></a>
  &nbsp;&nbsp;
  <a href=".github/assets/screenshots/mobile-stack-detail.png"><img src=".github/assets/screenshots/mobile-stack-detail.png" alt="Mobile stack detail" width="220"></a>
  &nbsp;&nbsp;
  <a href=".github/assets/screenshots/mobile-secrets.png"><img src=".github/assets/screenshots/mobile-secrets.png" alt="Mobile secrets management" width="220"></a><br>
  <sub><b>Mobile-friendly UI</b> — manage stacks and secrets from your phone</sub>
</p>

## Project Scope

Targets developers, homelabs, and self-hosters running plain `docker compose` stacks across one or many hosts and repos — GitOps sync/deploy, without manifest sprawl or enterprise-grade autoscaling/control-plane complexity.

**Not** a Kubernetes/Swarm alternative, not a competitor to Flux/ArgoCD or enterprise app-management platforms. For production workloads exposed to the internet at enterprise scale (multi-node autoscaling, high availability, compliance-grade orchestration), prefer Kubernetes (with Flux/ArgoCD) instead.

## Tech Stack

- **Backend**: Go + PocketBase
- **Frontend**: Nuxt 4 + Vue 3 + Nuxt UI
- **Container Runtime**: Docker + Docker Compose
- **Database**: SQLite (via PocketBase)

## Requirements

- A Linux host on `amd64` or `arm64`.
- Docker Engine `25.0+` and Docker Compose `v2.24.1+` (`docker compose`).
- Worker access to the Docker socket or configured Docker endpoint.
- Network access from every worker to the server; use TLS or a private network for remote workers.

Keep the server and every worker on the exact same release. See the
[compatibility policy](docs/COMPATIBILITY.md) before upgrading either component.

## Quick Start

wireops has two pieces you bring up separately: the **server** first, then one or more **workers** that register against it (see the [Architecture wiki page](https://github.com/wireops/wireops/wiki/Architecture) for why). The `example/docker-compose.yml` file has both services predefined.

**1. Start the server**

Generate a 32-byte key for `SECRET_KEY` (this encrypts every secret wireops stores — git passwords, SSH keys, integration tokens — so treat it like any other production credential):

```bash
openssl rand -hex 32
```

```bash
cp example/.env.example example/.env
# Edit example/.env: set SECRET_KEY, BOOTSTRAP_TOKEN, WORKER_TAGS, and the
# Linux DOCKER_GID when applicable
# Optionally set APP_URL for production deployments

cd example
docker compose up -d wireops
```

Minimum `example/.env` values for the server's first start:

```bash
SECRET_KEY=<paste the openssl output here>
BOOTSTRAP_TOKEN=replace-with-a-strong-one-time-token
```

**2. Create the first administrator account**

There are no default credentials — wireops requires a bootstrap token for the first web-based setup. Open `http://localhost:8090/setup` (or `http://<server-ip>:8090/setup`), enter the `BOOTSTRAP_TOKEN` you set, and create the admin account. The setup route closes itself automatically once that first admin exists.

Notes:
- `BOOTSTRAP_TOKEN` is only used while no administrator exists yet — if it's missing on a fresh instance, the setup page stays blocked until it's configured.
- After setup is complete, keeping or removing `BOOTSTRAP_TOKEN` doesn't reopen setup, but removing it is recommended.

**3. Issue a worker token**

Once logged in, go to **Workers → Add Worker** in the UI and copy the token it gives you.

**4. Start a worker**

```bash
# in the same example/ directory
WORKER_TOKEN=paste-the-token-here WORKER_TAGS=prod,eu-west-1 docker compose up -d wireops-worker
```

The worker connects out to the server, registers with that token, and starts polling for stacks/jobs to run. Check **Workers** in the UI — it should flip to `ACTIVE` within a few seconds.

`WORKER_TAGS` labels what a worker is for (e.g. `prod`, `eu-west-1`) — required, comma-separated, set on the worker itself, not in the UI. Stacks and jobs can require specific tags in their `wireops.yaml`/`job.yaml`, so only matching workers show up as valid targets.

> **Linux permissions:** the containers run as UID/GID `1000`. Before the first start, create `example/data` and make it writable by that identity. A Linux worker also needs the Docker socket's numeric group in `DOCKER_GID`; `example/.env.example` contains the commands. On Docker Desktop, the data directory must still be writable by the container user and socket permissions depend on the local setup; Linux is the supported production environment.

The example pins `WIREOPS_VERSION` instead of using `latest`. Port `8443` is plain HTTP/WebSocket while `TLS_ENABLED=false`; remote workers should use native TLS or a private network. See [Production](docs/PRODUCTION.md) before exposing an instance.

## Usage

1. **Add a Repository**: Configure your Git repository with credentials if needed
2. **Create a Stack**: Link a stack to a repository, specify compose file location
3. **Configure**: Set environment variables, poll interval, and sync options
4. **Deploy**: Stacks auto-sync on interval or trigger manually/via webhook

### Scheduled Jobs

wireops supports cron-based execution of one-shot Docker containers. A job is configured via a `job.yaml` file committed to a Git repository.

The `job.yaml` configuration is the single source of truth for the job and requires a `resources` block with `cpu`, `memory`, and `timeout` settings.

**Example `job.yaml`:**

```yaml
title: "Database Backup"
description: "Nightly backup of the postgres database"
cron: "0 2 * * *"
image: "postgres:15-alpine"
command: ["pg_dump", "-h", "db", "-U", "postgres", "mydb"]
tags: ["backup", "prod"]
mode: "once" # once or once_all
volumes:
  - "/opt/backups:/backups"
network: "prod_network"
resources:
  cpu: "0.5"        # Mandatory: CPU limit (e.g., "0.5" or "2")
  memory: "512m"    # Mandatory: Memory limit (e.g., "256m" or "1g")
  timeout: "15m"    # Mandatory: Job timeout duration (e.g., "10m", "1h")
```

### Private Registry Credentials

To deploy a stack that pulls images from a private registry (Docker Hub, GHCR, a self-hosted Harbor/Nexus/`registry:2`, or GCP Artifact Registry), add a **Registry Credential** and assign it to the stack — the worker authenticates the pull with it, scoped to that one deploy only, without touching the worker host's global `~/.docker/config.json`.

1. **Settings → Secrets → Registry Credentials** tab, **Add Credential**.
2. Fill in:
   - **Registry URL** — a host, with or without scheme (`ghcr.io`, `https://registry.example.com:5000`).
   - **Type** — `Username / Password`, `Token`, or `GCP Service Account` (paste the service-account JSON key; the username is fixed to `_json_key`, the convention Google's registries expect).
   - **Insecure registry** — only for a registry served over plain HTTP or a self-signed TLS certificate. This toggle does **not** configure the worker's Docker daemon for you — the worker host still needs the registry listed under `insecure-registries` in its own `/etc/docker/daemon.json` (with Docker restarted), or the deploy will fail with a clear preflight error rather than a silent pull failure.
3. **Test Connection** before saving to confirm the credential authenticates against the registry.
4. Open the target **Stack**'s settings and assign the credential under **Registry Credential** — it now applies to every deploy, redeploy, and rollback of that stack.

Testing the connection to a registry on your own private network (not reachable from the public internet) requires opting it into `ALLOWED_PRIVATE_IP_RANGES` (comma-separated CIDRs/IPs) — by default the server blocks that request as a potential SSRF vector, the same protection already applied to git host key scanning. This only gates the server-side "Test Connection" handshake; the worker's actual pull is unaffected.

## Customization

### Custom Container Thumbnails

You can customize the image slug (icon/identifier) displayed in the UI for your containers by adding the `customization.image.slug` label to your Docker Compose services.

These images are fetched from the [selfh.st/icons](https://selfh.st/icons/) catalog and served globally via its CDN. The slug you provide must match the identifier used in their catalog.

The application automatically extracts this value from the service's `labels`, `annotations`, `deploy.labels`, or `deploy.annotations`.

**Example `docker-compose.yml`:**

```yaml
services:
  app:
    image: nginx:1.31.4-alpine
    labels:
      - "customization.image.slug=nginx"
```

### Override Deployment

Stacks support render-time overrides — swapping a service's `image`, `ports`, `networks`, or `scale` without committing anything to git. Overrides are stored on the stack (not the repo), applied only when the compose file is rendered for deploy, and only take effect the next time the stack is rendered/redeployed. A scale of `0` stops every container for that service; scale-only overrides adjust replica counts without recreating unchanged containers.

- Gated by the **"Allow render overrides"** worker policy flag (`allow_render_overrides`), off by default — set it globally or per-worker in worker policy settings.
- View, set, and clear overrides from the stack detail page, or via `GET`/`PUT`/`DELETE /api/custom/stacks/{id}/render-overrides`.
- Overridden values still pass through the worker's other deploy policy checks (image/network allowlists, etc.) — an override can't be used to bypass policy.
- Applying or clearing overrides force-recreates the stack's containers, except when the change is scale-only — those adjust replica counts without recreating unchanged containers.

## Environment Variables

`SECRET_KEY` is required. `BOOTSTRAP_TOKEN` is required only until the first
administrator is created — see [Quick Start](#quick-start) above.

For the full server/worker env var reference, SMTP, OIDC/SSO, secret providers (Vault/Infisical), and SOPS+age, see the [Environment Variables wiki page](https://github.com/wireops/wireops/wiki/Environment-Variables).

Metrics, Prometheus/Grafana scrape config, the MCP server, and the disaster-recovery runbook have also moved to the wiki: [Observability](https://github.com/wireops/wireops/wiki/Observability) · [MCP Server](https://github.com/wireops/wireops/wiki/MCP-Server) · [Disaster Recovery](https://github.com/wireops/wireops/wiki/Disaster-Recovery).

## Production

The quick start is intended to prove the installation. Before using wireops for workloads you care about, pin the image version, enable TLS for remote workers, configure off-host backups, protect `SECRET_KEY` separately, and test one restore. Follow the [production checklist](docs/PRODUCTION.md), [compatibility policy](docs/COMPATIBILITY.md), and [upgrade guide](docs/UPGRADING.md).

## Integrations

Wireops ships two kinds of integration plugin, registered the same way (`internal/integrations/`) and enabled/configured from the same Settings screen: **container-action** integrations (Traefik, Caddy, Nginx Proxy Manager, Dozzle) add clickable "Open"/"Logs" shortcuts to the container list; **notification** integrations (Webhook, Discord, Slack, Ntfy) send a message when a sync event fires and don't touch the container list.

Full plugin interface, config fields, and label/compose examples: [Integrations wiki page](https://github.com/wireops/wireops/wiki/Integrations).

## Development

```bash
# Backend
go run main.go serve

# Frontend
cd frontend
npm install
npm run dev
```

---

## Known Limitations

- No OCI-artifact source, Docker Swarm/multi-node, or canary/preview deploys yet (tracked as strategic backlog with no ETA).

## License

AGPLv3 — see [LICENSE](LICENSE)

wireops is free/open-source software. Forks and derivative works must also
be released under AGPLv3 (or a compatible license) and must credit
**wireops** ([github.com/wireops/wireops](https://github.com/wireops/wireops))
as the original project — see [NOTICE](NOTICE).

## Contributing

Contributions are welcome! Please open an issue or PR. Security reports must follow [SECURITY.md](SECURITY.md) instead of a public issue. User-visible changes should be added to the `Unreleased` section of [CHANGELOG.md](CHANGELOG.md).
