# wireops

GitOps controller for Docker Compose stacks. Automatically sync and deploy your compose stacks from Git repositories, similar to Flux/ArgoCD for Kubernetes.

## Features

- 🔄 Automatic synchronization from Git repositories
- 🐳 Docker Compose stack management
- 📊 Real-time container monitoring
- 🔐 Encrypted credentials (SSH keys, passwords)
- 🌐 Webhook support for CI/CD integration
- 📝 Environment variable management
- 🔄 Rollback to previous commits
- 🚀 Force redeploy with recreate options

## Tech Stack

- **Backend**: Go + PocketBase
- **Frontend**: Nuxt 3 + Vue 3 + Nuxt UI
- **Container Runtime**: Docker + Docker Compose
- **Database**: SQLite (via PocketBase)

## Quick Start

```bash
# Copy the example environment file
cp .env.example .env

# Edit .env and set your SECRET_KEY (generate with: openssl rand -hex 32)
# Optionally configure APP_URL for production deployments

# Run with Docker Compose
docker-compose up -d

# Or run directly
go run main.go serve
```

Access the UI at `http://localhost:8090`

Default credentials:
- Email: `admin@example.com`
- Password: `admin123456`

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

## Environment Variables

- `SECRET_KEY`: 32-byte key for encrypting credentials (required)
- `APP_URL`: Public URL of the application for CORS, webhooks, and image URLs (default: `http://localhost:8090`)
- `PB_DATA_DIR`: PocketBase data directory (default: `./pb_data`)
- `REPOS_WORKSPACE`: Directory for cloned repositories (default: `./repos`)
- `PORT`: Port to bind the server (default: `8090`)

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

Wireops supports integrations that add external platform actions and shortcuts directly into the application's container list. By analyzing specific properties or labels on your containers, wireops can expose quick actions.

To use an integration, you need to enable and configure it in the application's UI settings. The integrations evaluate your active containers on-the-fly.

Currently supported integrations:

### Traefik

The Traefik integration reads Traefik HTTP router rules from container labels and generates an "Open" action linking straight to the configured host.

- **Category**: Reverse Proxy
- **Label required**: `traefik.http.routers.<name>.rule=Host(...)`
- **Config**: You can customize the default `scheme` (e.g. `https`) and `port` (e.g. `443` or blank for default) when enabling the integration.
- **Example**: If a container has the label `traefik.http.routers.myapp.rule=Host(`myapp.example.com`)`, an action to open `https://myapp.example.com` is generated.

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

## Backlog / Future Enhancements

### 🎓 Onboarding Experience
- Interactive tour for first-time users
- Step-by-step wizard for stack creation
- Better empty states with actionable CTAs
- Preview compose file before creating stack

### 📋 Logs & Debugging
- Advanced log viewer with syntax highlighting
- Search/filter within logs
- Auto-scroll toggle and log streaming
- Download logs functionality
- Diff viewer for commit comparisons
- Real-time log streaming using SSE endpoint

### 🔄 Bulk Operations
- Multi-select stacks with checkboxes
- Bulk actions: "Sync All", "Pause All", "Resume All"
- Progress tracking for batch operations
- "Sync All Active Stacks" button on dashboard

### 🌍 Environment Variables Management
- Bulk edit mode (text editor format KEY=VALUE)
- Import/export .env files
- Copy env vars between stacks
- Templates for common variables
- Auto-complete for variable keys
- Detect required variables from compose file
- Highlight unused variables

### 🐳 Container Management
- "Restart All" / "Stop All" buttons per service
- Better container stats with fallback handling
- Improved container logs (pagination, more lines)
- Follow logs mode (auto-scroll)
- Bulk container operations

### ⚙️ User Preferences
- Configurable auto-refresh interval
- Default poll interval for new stacks
- Theme preferences
- Notification settings
- UI density options (compact/comfortable)

---

## License

MIT

## Contributing

Contributions are welcome! Please open an issue or PR.
