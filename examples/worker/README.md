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

## Prerequisites

1. A running wireops server reachable from this host
2. A **bootstrap token** — generated from the wireops admin panel under **Agents → New Agent → Copy Token**

The agent will:
1. Use the token once to obtain a signed mTLS certificate from the server's CA
2. Store the certificate in the `wireops-agent-pki` volume
3. Use the certificate for all future connections (token not needed again)

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
| `WIREOPS_MTLS_SERVER` | ✅ | — | mTLS URL of the wireops server (e.g. `https://host:8443`) |
| `WIREOPS_BOOTSTRAP_TOKEN` | First run only | — | One-time token from the admin panel |
| `AGENT_HOSTNAME` | — | container hostname | Label shown in the wireops UI |
| `WIREOPS_AGENT_PKI_DIR` | — | `/var/lib/wireops/pki` | Where certs are stored (must match volume mount) |
| `WIREOPS_AGENT_WORK_DIR` | — | `/tmp/wireops-agent` | Temp directory for rendered compose files |

> [!IMPORTANT]
> After the first successful boot the bootstrap token is no longer needed.  
> Remove `WIREOPS_BOOTSTRAP_TOKEN` from `.env` before the next restart — keeping it is harmless but unnecessary.
