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
