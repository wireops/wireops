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

## Features

- 🔄 Automatic synchronization from Git repositories
- 🐳 Docker Compose stack management
- 📊 Real-time container monitoring (with worker runtime info and container ports)
- 🔐 Encrypted credentials (SSH keys, passwords) + pluggable secret providers (internal AES-GCM, HashiCorp Vault, Infisical)
- 🛡️ Role-based access control (viewer/operator/admin/monitoring) and audit logging
- 🚧 Worker-side deploy security policies (block privileged/host-network/docker.sock/host-PID/host-IPC)
- 🔑 SSO login via any OIDC provider
- 🌐 Webhook, Discord, Slack, and ntfy notifications
- 📝 Environment variable management
- 🔄 Rollback to previous commits
- 🚀 Force redeploy with recreate options
- 🗓️ Cron-scheduled one-shot Docker jobs (`job.yaml`)

## Tech Stack

- **Backend**: Go + PocketBase
- **Frontend**: Nuxt 4 + Vue 3 + Nuxt UI
- **Container Runtime**: Docker + Docker Compose
- **Database**: SQLite (via PocketBase)

## Quick Start

```bash
# Copy the example environment file
cp .env.example .env

# Edit .env and set your SECRET_KEY (generate with: openssl rand -hex 32)
# Set a BOOTSTRAP_TOKEN for the first administrator setup
# Optionally configure APP_URL for production deployments

# Run with Docker Compose
docker-compose up -d

# Or run directly
go run main.go serve
```

Access the UI at `http://localhost:8090`

There are no default credentials. On a fresh instance, open `/setup` and create the first administrator account using the `BOOTSTRAP_TOKEN` you configured.

### Initial Setup

Wireops requires a bootstrap token for the first web-based setup.

1. Set `BOOTSTRAP_TOKEN` before starting the server.
2. Open `http://localhost:8090/setup` or `http://<server-ip>:8090/setup`.
3. Enter the bootstrap token and create the first administrator account.
4. After the first admin is created, the setup route is automatically closed.

Example:

```bash
SECRET_KEY=replace-with-32-byte-key
BOOTSTRAP_TOKEN=replace-with-a-strong-one-time-token
```

Notes:
- `BOOTSTRAP_TOKEN` is only used while no administrator exists yet.
- If `BOOTSTRAP_TOKEN` is missing on a fresh instance, the setup page will stay blocked until it is configured.
- After setup is complete, keeping or removing `BOOTSTRAP_TOKEN` does not reopen setup, but removing it is recommended.

## Architecture

```
┌─────────────┐     ┌──────────────┐     ┌─────────────┐
│   Frontend  │────▶│   PocketBase │────▶│  Scheduler  │
│  (Nuxt UI)  │     │   (REST API) │     │   (Cron)    │
└─────────────┘     └──────────────┘     └──────┬──────┘
                                                 │
                                                 ▼
                    ┌──────────────┐     ┌─────────────┐
                    │ Git Repos    │◀────│  Reconciler │
                    │ (Cloned)     │     │  (Sync)     │
                    └──────────────┘     └──────┬──────┘
                                                 │
                                                 ▼
                                         ┌─────────────┐
                                         │   Docker    │
                                         │  Compose    │
                                         └─────────────┘
```

## Usage

1. **Add a Repository**: Configure your Git repository with credentials if needed
2. **Create a Stack**: Link a stack to a repository, specify compose file location
3. **Configure**: Set environment variables, poll interval, and sync options
4. **Deploy**: Stacks auto-sync on interval or trigger manually/via webhook

### Container Image Customization

You can customize the image slug (icon/identifier) displayed in the UI for your containers by adding the `customization.image.slug` label to your Docker Compose services. 

These images are fetched from the [selfh.st/icons](https://selfh.st/icons/) catalog and served globally via its CDN. The slug you provide must match the identifier used in their catalog.

The application automatically extracts this value from the service's `labels`, `annotations`, `deploy.labels`, or `deploy.annotations`.

**Example `docker-compose.yml`:**

```yaml
services:
  app:
    image: my-app:latest
    labels:
      - "customization.image.slug=nuxtjs"
    # Alternatively, you can use deploy labels/annotations:
    # deploy:
    #   labels:
    #     - "customization.image.slug=nuxtjs"
```

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

## Environment Variables

### Server

| Variable | Required | Default | Description |
|---|---|---|---|
| `SECRET_KEY` | **Yes** | — | 32-byte AES key for encrypting credentials and secrets at rest. Generate with `openssl rand -hex 32` |
| `BOOTSTRAP_TOKEN` | **Yes** for first-time setup | — | One-time bootstrap secret required to create the first administrator account from the `/setup` page |
| `APP_URL` | No | `http://localhost:8090` | Base URL used for CORS, webhook URLs, and emails |
| `PORT` | No | `8090` | HTTP port for the UI, REST API, and Prometheus metrics (`/metrics`) |
| `PB_DATA_DIR` | No | `./pb_data` | PocketBase data directory (SQLite database, uploads) |
| `REPOS_WORKSPACE` | No | `./repos` | Directory where Git repositories are cloned |
| `STACKS_STORAGE_PATH` | No | `{PB_DATA_DIR}/stacks` | Directory for rendered compose revision files |
| `HEARTBEAT_INTERVAL` | No | `30` | Heartbeat interval in seconds. Remote worker read deadline is 3x this value |
| `ALLOWED_PRIVATE_IP_RANGES` | No | — | Comma-separated CIDR ranges allowed for SSH host key scanning |

#### SMTP (optional)

| Variable | Default | Description |
|---|---|---|
| `SMTP_HOST` | — | SMTP server host. When set, enables PocketBase email delivery |
| `SMTP_PORT` | `587` | SMTP server port |
| `SMTP_USERNAME` | — | SMTP authentication username |
| `SMTP_PASSWORD` | — | SMTP authentication password |
| `SMTP_SENDER` | — | Sender email address |
| `SMTP_TLS` | `false` | Set to `true` to enable TLS for SMTP |

#### OIDC / SSO (optional)

wireops supports SSO login via any OIDC-compatible provider (Keycloak, Authentik, Zitadel, Okta, etc.). When configured, a **"Continue with [name]"** button appears on the login page alongside the standard email/password form.

| Variable | Required | Description |
|---|---|---|
| `OIDC_CLIENT_ID` | **Yes** (to enable) | OAuth2 Client ID from your identity provider |
| `OIDC_CLIENT_SECRET` | **Yes** (to enable) | OAuth2 Client Secret |
| `OIDC_AUTH_URL` | **Yes** (to enable) | Authorization endpoint of your IdP |
| `OIDC_TOKEN_URL` | **Yes** (to enable) | Token endpoint of your IdP |
| `OIDC_USER_INFO_URL` | No | UserInfo endpoint. If omitted, user data is read from the `id_token` claims |
| `OIDC_DISPLAY_NAME` | No | Label shown on the login button (default: `SSO`) |

> **Note on special characters:** If `OIDC_CLIENT_SECRET` (or any value) contains special characters (`$`, `%`, `*`, `!`, etc.), wrap it in **single quotes** in the `.env` file to prevent `godotenv` from interpreting them:
> ```bash
> OIDC_CLIENT_SECRET='my$ecret!@#%'
> ```

The **redirect/callback URL** to register in your identity provider is:
```
https://your-wireops-domain.com/api/oauth2-redirect
```

**Provider example:**

```bash
# Authentik
OIDC_CLIENT_ID=wireops
OIDC_CLIENT_SECRET=your-secret
OIDC_AUTH_URL=https://authentik.example.com/application/o/wireops/authorize/
OIDC_TOKEN_URL=https://authentik.example.com/application/o/token/
OIDC_USER_INFO_URL=https://authentik.example.com/application/o/userinfo/
OIDC_DISPLAY_NAME=Authentik
```

> [!WARNING]
> **SSO and the Initial Admin Account**
>
> If you log in via SSO using the **exact same email address** that you used to create the initial wireops instance (the first protected admin account), the system will automatically link your local account to the SSO identity.
>
> When this happens, the wireops frontend **will forcibly override your local role** with whatever role your identity provider (IdP) assigns you. If your IdP maps your account to a lesser role (like `viewer`), you will lose your administrative privileges inside the wireops UI. 
> 
> **Make absolutely sure** that your initial admin email is mapped to the `admin` role in your IdP before logging in via SSO.

### Worker

| Variable | Required | Default | Description |
|---|---|---|---|
| `SERVER_URL` | **Yes** | — | URL of the wireops server (e.g. `https://wireops.example.com:8443`) |
| `WORKER_TOKEN` | **Yes** | — | Worker registration and authentication token |
| `HOSTNAME` | No | System hostname | Worker identifier sent during registration |
| `WORKER_TAGS` | No | — | Comma-separated tags for job routing (e.g. `gpu,us-east`) |
| `HEARTBEAT_INTERVAL` | No | `30` | Interval in seconds between heartbeats sent to the server |
| `WORKER_STACK_DIR` | No | `<os.TempDir()>/wireops` | Directory where the worker writes temporary compose files |
| `WORKER_TLS_SKIP_VERIFY` | No | `false` | Skip TLS certificate verification. Set to `true` when the server uses a self-signed certificate |

### Secret Providers

Every secret-flagged env var (stack, global, or job-scoped) picks a `secret_provider` at creation time: `internal` (default), `vault`, or `infisical`.

| Provider | Where the secret lives | Value stored on the env var |
|---|---|---|
| `internal` | Encrypted at rest in wireops's own DB (AES-GCM, `SECRET_KEY`) | The plaintext secret, encrypted |
| `vault` | HashiCorp Vault (KV v2) | A reference: `<mount>/data/<path>#<field>` |
| `infisical` | Infisical | A reference: `<project-id>/<environment>/<secret-path>#<SECRET_NAME>` |

Vault and Infisical are **not** configured via server env vars — enable and configure them from **Settings → Integrations** (category "Secret Backend"):

- **Vault**: `address`, `token`, optional `allowed_mount` (scopes which mount the picker/resolver may touch). The token is encrypted at rest using the existing `SECRET_KEY`.
- **Infisical**: `site_url` (defaults to `https://app.infisical.com`), `client_id`, `client_secret`, optional `allowed_project_id`. The client secret is encrypted at rest using the existing `SECRET_KEY`.

Once a backend is enabled, its provider option appears in the env var editor; picking `vault` or `infisical` swaps the value field for a guided picker (mount/path/field, or project/environment/path/secret) that resolves against Vault/Infisical server-side — you never type or see a raw reference string by hand, and existing secret values are never displayed in plaintext.

Notes:
- **A secret's provider is locked once saved.** You can't switch an existing secret from `internal` to `vault` (or vice versa) — delete it and recreate it with the new provider instead.
- **Disabled backends are caught before deploy.** If a stack or job references a `vault`/`infisical` env var whose backend integration is disabled or unconfigured, sync/job execution fails fast with an error naming the provider and the affected keys, instead of failing mid-deploy.

### APP_URL Configuration

The `APP_URL` variable is used to:
- Configure CORS for frontend access
- Generate webhook URLs for CI/CD integration
- Serve future image and media assets

**Format**: `scheme://host[:port]` (no trailing slash or path)

**Examples**:
```bash
# Local development
APP_URL=http://localhost:8090

# Production with domain
APP_URL=https://wireops.example.com

# Custom port
APP_URL=http://192.168.1.100:8090
```

**Note**: When using `localhost` or `127.0.0.1`, the application automatically allows common development ports (3000, 5173) for CORS.

## Observability

Wireops exposes a single **operational port** (`PORT`, default `8090`) for the UI, REST API, and Prometheus metrics. Remote workers connect on a separate **worker port** (`TLS_WORKER_PORT`, default `8443`) — scrapers should not target that port.

| Port | Purpose |
|---|---|
| `PORT` (8090) | UI, `/api/custom/*`, `GET /metrics` |
| `TLS_WORKER_PORT` (8443) | Worker WebSocket and registration only |

### Metrics

| Endpoint | Description |
|---|---|
| `GET /metrics` | Aggregated worker metrics (canonical scrape path) |
| `GET /api/custom/metrics` | Alias of `/metrics` |
| `GET /api/custom/workers/{id}/metrics` | Metrics from a single connected worker |

**Authentication:** create a **service account** with role `monitoring` (Settings → Service Accounts), generate an **API key**, and send it on every scrape via the `X-Wireops-Api-Key` header. Requests without a valid key receive `401`; keys tied to roles below `monitoring` receive `403`.

**Quick test:**

```bash
curl -H "X-Wireops-Api-Key: wireops_sk_..." http://localhost:8090/metrics
```

#### Prometheus (`prometheus.yml`)

Prometheus must send `X-Wireops-Api-Key` (not `Authorization: Bearer`). Use `http_headers` (Prometheus **2.45+**):

```yaml
scrape_configs:
  - job_name: wireops
    metrics_path: /metrics
    scheme: http
    scrape_interval: 30s
    static_configs:
      - targets:
          - wireops-host:8090   # PORT — same host/port as the UI
    http_headers:
      X-Wireops-Api-Key:
        values:
          - wireops_sk_your_api_key_here

    # Optional: skip TLS verification only when APP_URL uses a self-signed cert
    # tls_config:
    #   insecure_skip_verify: true
```

When `APP_URL` uses HTTPS, set `scheme: https` and point `targets` at the same host/port users open in the browser (for example `wireops.example.com:443`).

Prefer injecting the API key from your secrets manager or an env-expanded config file instead of committing it to git.

#### Grafana Agent / Alloy (alternative)

```hcl
prometheus.scrape "wireops" {
  targets      = [{ __address__ = "wireops-host:8090" }]
  forward_to   = [prometheus.remote_write.default.receiver]
  metrics_path = "/metrics"
  scheme       = "http"

  authorization {
    type             = "Header"
    credentials_file = "/etc/wireops/api-key"
  }
}
```

Store the header line in `credentials_file`:

```text
X-Wireops-Api-Key: wireops_sk_...
```

## Development

```bash
# Backend
go run main.go serve

# Frontend
cd frontend
npm install
npm run dev
```

## Integrations

Wireops ships two kinds of integration plugin, registered the same way (`internal/integrations/`) and enabled/configured from the same Settings screen, but functionally distinct:

- **Container-action integrations** (Reverse Proxy / Logging) inspect your running containers' labels and add clickable shortcuts to the container list — e.g. an "Open" link or a "Logs" link.
- **Notification integrations** (category `Notification`) do **not** touch the container list at all — they only send a message when a sync event fires (`sync.started`, `sync.done`, `sync.error`, `sync.test`). Their `ResolveContainerActions` is a no-op by design.

### Container-action integrations

| Integration | Category | What it adds |
|---|---|---|
| Traefik | Reverse Proxy | "Open" action from router host rule labels |
| Caddy | Reverse Proxy | "Open" action from Caddy labels |
| Nginx Proxy Manager | Reverse Proxy | "Open" action for NPM-fronted containers |
| Dozzle | Logging | "Logs" action linking to a Dozzle instance |

### Notification integrations (sync events only, no container actions)

| Integration | What it does |
|---|---|
| Webhook | HMAC-signed HTTP POST on sync events |
| Discord | Sync event messages to a Discord channel |
| Slack | Sync event messages to a Slack channel |
| Ntfy | Push notifications via ntfy.sh |

Details for two container-action integrations (Traefik, Dozzle) are documented in depth below as examples; the rest follow the same enable-in-Settings pattern.

### Traefik

The Traefik integration reads Traefik HTTP router rules from container labels and generates an "Open" action linking straight to the configured host.

- **Category**: Reverse Proxy
- **Label required**: `traefik.http.routers.<name>.rule=Host(...)`
- **Config**: You can customize the default `scheme` (e.g. `https`) and `port` (e.g. `443` or blank for default) when enabling the integration.
- **Example**: If a container has the label ``traefik.http.routers.myapp.rule=Host(`myapp.example.com`)``, an action to open `https://myapp.example.com` is generated.

**Example `docker-compose.yml` with Nginx:**

```yaml
services:
  web:
    image: nginx:latest
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.my-nginx.rule=Host(`nginx.example.com`)"
      - "traefik.http.services.my-nginx.loadbalancer.server.port=80"
```

### Dozzle

The Dozzle integration replaces the basic container log viewer by redirecting the user to your centrally deployed Dozzle logging instance.

- **Category**: Logging
- **Config**: Needs the base `url` to your Dozzle instance (e.g., `https://logs.example.com`).
- **Action generated**: A "Dozzle Logs" action is created for all containers linking to `{baseURL}/container/{containerID}`. No container-specific labels are required as long as Dozzle can access your Docker socket.

**Example `docker-compose.yml` with Nginx:**

```yaml
services:
  web:
    image: nginx:latest
    # No extra labels needed! Dozzle automatically 
    # connects to the docker socket and wireops will
    # automatically generate the action linking to
    # https://logs.example.com/container/<nginx-container-id>
```

---

## Known Limitations

- **`internal/backup`** (config/data backup & restore) has test scaffolding but no shipped implementation.
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
- SOPS+age support
- Git auth hardening, deploy metrics/alerts
- Audited web terminal (blocked on RBAC completeness)
- docker-run → compose converter, OCI artifact sources, Swarm/multi-node, canary deploys

---

## License

GPLv3 — see [LICENSE](LICENSE)

## Contributing

Contributions are welcome! Please open an issue or PR.
