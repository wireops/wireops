# Use Case 2: Standalone Agent

In this mode you run **only the wireops agent** on a remote host.  
The agent connects to a central wireops server over a mutually-authenticated TLS WebSocket and executes `docker compose up` commands on behalf of the server.

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
           │  mTLS WebSocket (port 8443)
           │  sends: base64 compose YAML + env vars
           ▼
  ┌────────────────────┐
  │  wireops-agent       │  ◀── this container
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
| `docker-compose.yml` | Agent compose file |
| `Dockerfile` | Multi-stage agent image |
| `.env.example` | Environment variable template |

## Quick start

```bash
# 1. Copy env template
cp .env.example .env

# 2. Fill in your server URLs and bootstrap token
nano .env

# 3. Start the agent
docker compose up -d

# 4. Verify it appears as ACTIVE in the wireops UI under Agents
```

## Environment variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `WIREOPS_SERVER` | ✅ | — | HTTP URL of the wireops server (for bootstrap) |
| `AGENT_HOSTNAME` | — | container hostname | Label shown in the wireops UI |
| `WIREOPS_AGENT_WORK_DIR` | — | `/tmp/wireops-agent` | Temp directory for rendered compose files |