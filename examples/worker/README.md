# Use Case 2: Standalone Agent

In this mode you run **only the wireops worker** on a remote host.  
The worker connects to a central wireops server over a token-authenticated WebSocket connection and executes `docker compose up` commands on behalf of the server.

## When to use this

- You want to manage stacks on **multiple hosts** from a single wireops server
- You have edge nodes, VMs, or bare-metal servers that need their own Docker daemon
- The server is hosted separately (cloud, VPS, etc.)

## Architecture

```
  wireops Server (elsewhere)
  ┌────────────────────┐
  │  UI + API          │
  │  Git + Scheduler   │
  │  Compose Renderer  │
  └────────┬───────────┘
           │  WebSocket (port 8443)
           │  sends: base64 compose YAML + env vars
           ▼
  ┌────────────────────┐
  │  wireops-worker    │  ◀── this container
  │  container         │
  └────────┬───────────┘
           │
           ▼
       Docker socket
     (docker compose up)
```

## Files

| File | Description |
|------|-------------|
| `docker-compose.yml` | Worker compose file |
| `Dockerfile` | Multi-stage worker image |
| `.env.example` | Environment variable template |

## Quick start

```bash
# 1. Copy env template
cp .env.example .env

# 2. Fill in your server URL and worker token
nano .env

# 3. Start the worker
docker compose up -d

# 4. Verify it appears as ACTIVE in the wireops UI under Workers
```

## Environment variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `WIREOPS_SERVER` | ✅ | — | URL of the wireops server (e.g. http://localhost:8443) |
| `WIREOPS_WORKER_TOKEN` | ✅ | — | Worker registration and authentication token |
| `WORKER_HOSTNAME` | — | container hostname | Optional name shown in the wireops UI |
| `WIREOPS_WORKER_WORK_DIR` | — | `/tmp/wireops-worker` | Temp directory for rendered compose files |