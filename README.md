<p align="center">
  <img src="frontend/app/assets/img/logo.png" alt="wireops logo" width="120">
</p>

# wireops

[![Latest Release](https://img.shields.io/github/v/release/wireops/wireops?sort=semver)](https://github.com/wireops/wireops/releases/latest)
[![Server CI](https://github.com/wireops/wireops/actions/workflows/server-ci.yml/badge.svg)](https://github.com/wireops/wireops/actions/workflows/server-ci.yml)
[![Worker CI](https://github.com/wireops/wireops/actions/workflows/worker-ci.yml/badge.svg)](https://github.com/wireops/wireops/actions/workflows/worker-ci.yml)
[![Known Vulnerabilities](https://snyk.io/test/github/wireops/wireops/badge.svg)](https://snyk.io/test/github/wireops/wireops)
[![Go Report Card](https://goreportcard.com/badge/github.com/wireops/wireops)](https://goreportcard.com/report/github.com/wireops/wireops)
[![Go Version](https://img.shields.io/github/go-mod/go-version/wireops/wireops)](go.mod)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](LICENSE)
[![CodeRabbit Pull Request Reviews](https://img.shields.io/coderabbit/prs/github/wireops/wireops?utm_source=oss&utm_medium=github&utm_campaign=wireops%2Fwireops&labelColor=171717&color=FF570A&link=https%3A%2F%2Fcoderabbit.ai&label=CodeRabbit+Reviews)](https://coderabbit.ai)
[![Codacy Badge](https://app.codacy.com/project/badge/Grade/cdc7bea4ca1e44f780110e784d34938a)](https://app.codacy.com/gh/wireops/wireops/dashboard?utm_source=gh&utm_medium=referral&utm_content=&utm_campaign=Badge_grade)
[![Codacy Badge](https://app.codacy.com/project/badge/Coverage/cdc7bea4ca1e44f780110e784d34938a)](https://app.codacy.com/gh/wireops/wireops/dashboard?utm_source=gh&utm_medium=referral&utm_content=&utm_campaign=Badge_coverage)

GitOps controller for Docker Compose stacks. Automatically sync and deploy your compose stacks from Git repositories, similar to Flux/ArgoCD for Kubernetes.

> **Project status**: pre-1.0, actively developed (releases `v0.1.x`). Core GitOps sync, worker security policies, RBAC, and external secret providers (Vault, Infisical) are in daily use; the audited web terminal is intentionally not started yet — see [Known Limitations](#known-limitations).

📚 **Full technical docs** (architecture, data model, API reference, env vars, MCP server, disaster recovery, integrations) live in the **[Wiki](https://github.com/wireops/wireops/wiki)**.

## Project Scope

Targets developers, homelabs, and self-hosters running plain `docker compose` stacks across one or many hosts and repos — GitOps sync/deploy, without manifest sprawl or enterprise-grade autoscaling/control-plane complexity. 

**Not** a Kubernetes/Swarm alternative, not a competitor to Flux/ArgoCD or enterprise app-management platforms. For production workloads exposed to the internet at enterprise scale (multi-node autoscaling, high availability, compliance-grade orchestration), prefer Kubernetes (with Flux/ArgoCD) instead.

## Table of Contents

- [Project Scope](#project-scope)
- [Features](#features)
- [Tech Stack](#tech-stack)
- [Quick Start](#quick-start)
- [Usage](#usage)
- [Customization](#customization)
- [Development](#development)
- [Known Limitations](#known-limitations)
- [Backlog / Future Enhancements](#backlog--future-enhancements)
- [License](#license)
- [Contributing](#contributing)

Deeper technical docs (moved to the [Wiki](https://github.com/wireops/wireops/wiki)): [Architecture](https://github.com/wireops/wireops/wiki/Architecture) · [Data Model](https://github.com/wireops/wireops/wiki/Data-Model) · [API Reference](https://github.com/wireops/wireops/wiki/API-Reference) · [Environment Variables](https://github.com/wireops/wireops/wiki/Environment-Variables) · [Observability](https://github.com/wireops/wireops/wiki/Observability) · [MCP Server](https://github.com/wireops/wireops/wiki/MCP-Server) · [Disaster Recovery](https://github.com/wireops/wireops/wiki/Disaster-Recovery) · [Integrations](https://github.com/wireops/wireops/wiki/Integrations)

## Features

- 🔄 Automatic synchronization from Git repositories
- 🐳 Docker Compose stack management
- 📊 Real-time container monitoring (with worker runtime info and container ports)
- 🔐 Encrypted credentials and environment variables (SSH keys, passwords, secrets) with pluggable integrations (internal AES-GCM, SOPS+age, HashiCorp Vault, Infisical)
- 🎛️ Optional render-time stack overrides (image/ports/networks) for one-off validation testing, without a git commit
- 🛡️ Strong policy and RBAC system, with audit logging
- 🚧 Worker-side deploy security policies (block privileged/host-network/docker.sock/host-PID/host-IPC)
- 🔑 SSO login via any OIDC provider
- 🌐 Webhook, Discord, Slack, and ntfy notifications
- 🔄 Rollback to previous commits
- 🚀 Force redeploy with recreate options
- 🗓️ Cron-scheduled one-shot Docker jobs (`job.yaml`)

## Tech Stack

- **Backend**: Go + PocketBase
- **Frontend**: Nuxt 4 + Vue 3 + Nuxt UI
- **Container Runtime**: Docker + Docker Compose
- **Database**: SQLite (via PocketBase)

## Quick Start

wireops has two pieces you bring up separately: the **server** first, then one or more **workers** that register against it (see the [Architecture wiki page](https://github.com/wireops/wireops/wiki/Architecture) for why). The `example/docker-compose.yml` file has both services predefined.

**1. Start the server**

Generate a 32-byte key for `SECRET_KEY` (this encrypts every secret wireops stores — git passwords, SSH keys, integration tokens — so treat it like any other production credential):

```bash
openssl rand -hex 32
```

```bash
cp .env.example .env
# Edit .env: set SECRET_KEY to the value generated above, and set BOOTSTRAP_TOKEN
# Optionally set APP_URL for production deployments

cd example
docker compose up -d wireops
# Or run directly: go run main.go serve
```

Example `.env` values:

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

Once logged in, go to **Workers → Generate Token** in the UI and copy the token it gives you.

**4. Start a worker**

```bash
# in the same example/ directory
WORKER_TOKEN=paste-the-token-here docker compose up -d wireops-worker
```

The worker connects out to the server, registers with that token, and starts polling for stacks/jobs to run. Check **Workers** in the UI — it should flip to `ACTIVE` within a few seconds.

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

## Customization

### Custom Container Thumbnails

You can customize the image slug (icon/identifier) displayed in the UI for your containers by adding the `customization.image.slug` label to your Docker Compose services.

These images are fetched from the [selfh.st/icons](https://selfh.st/icons/) catalog and served globally via its CDN. The slug you provide must match the identifier used in their catalog.

The application automatically extracts this value from the service's `labels`, `annotations`, `deploy.labels`, or `deploy.annotations`.

**Example `docker-compose.yml`:**

```yaml
services:
  app:
    image: nginx:latest
    labels:
      - "customization.image.slug=nginx"
```

### Override Deployment

Stacks support render-time overrides — swapping a service's `image`, `ports`, or `networks` without committing anything to git. Overrides are stored on the stack (not the repo), applied only when the compose file is rendered for deploy, and only take effect the next time the stack is rendered/redeployed.

- Gated by the **"Allow render overrides"** worker policy flag (`allow_render_overrides`), off by default — set it globally or per-worker in worker policy settings.
- View, set, and clear overrides from the stack detail page, or via `GET`/`PUT`/`DELETE /api/custom/stacks/{id}/render-overrides`.
- Overridden values still pass through the worker's other deploy policy checks (image/network allowlists, etc.) — an override can't be used to bypass policy.
- Applying or clearing overrides force-recreates the stack's containers.

## Environment Variables

`SECRET_KEY` and `BOOTSTRAP_TOKEN` are required — see [Quick Start](#quick-start) above. 

For the full server/worker env var reference, SMTP, OIDC/SSO, secret providers (Vault/Infisical), and SOPS+age, see the [Environment Variables wiki page](https://github.com/wireops/wireops/wiki/Environment-Variables).

Metrics, Prometheus/Grafana scrape config, the MCP server, and the disaster-recovery runbook have also moved to the wiki: [Observability](https://github.com/wireops/wireops/wiki/Observability) · [MCP Server](https://github.com/wireops/wireops/wiki/MCP-Server) · [Disaster Recovery](https://github.com/wireops/wireops/wiki/Disaster-Recovery).

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

- **Audited web terminal**: intentionally not started — requires the RBAC system to be fully wired first to avoid shipping a high-risk feature half-done.
- No OCI-artifact source, Docker Swarm/multi-node, or canary/preview deploys yet (tracked as strategic backlog with no ETA).

## Backlog / Future Enhancements

### 🎓 Onboarding Experience
- Interactive tour for first-time users
- Step-by-step wizard for stack creation
- Better empty states with actionable CTAs
- Preview compose file before creating stack

### 📋 Logs & Debugging
- Advanced log viewer with syntax highlighting
- Search/filter within logs
- Download logs functionality
- Diff viewer for commit comparisons

### 🔄 Bulk Operations
- Multi-select stacks with checkboxes
- Bulk actions: "Sync All", "Pause All", "Resume All"
- Progress tracking for batch operations

### 🌍 Environment Variables Management
- Bulk edit mode (text editor format KEY=VALUE)
- Import/export .env files
- Copy env vars between stacks
- Templates for common variables
- Detect required variables from compose file

### 🐳 Container Management
- "Restart All" / "Stop All" buttons per service
- Bulk container operations

### ⚙️ User Preferences
- Configurable auto-refresh interval
- Theme preferences
- UI density options (compact/comfortable)

### 🔒 Security & Ops (strategic)
- Git auth hardening, deploy metrics/alerts
- Audited web terminal (blocked on RBAC completeness)
- docker-run → compose converter, OCI artifact sources, Swarm/multi-node, canary deploys

---

## License

GPLv3 — see [LICENSE](LICENSE)

## Contributing

Contributions are welcome! Please open an issue or PR.
