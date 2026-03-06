# Use Case 1: Server with Embedded Agent

In this mode the wireops **server** runs its own built-in agent.  
The server handles git polling, compose rendering, scheduling, **and** executes `docker compose up` directly on the same host.

This is the simplest deployment — everything runs in a single container.

## When to use this

- You manage all your stacks on the **same machine** as the server
- You don't need remote agents
- You want minimal infrastructure

## Architecture

```
┌─────────────────────────────────────────┐
│  wireops container                        │
│                                         │
│  ┌──────────┐    ┌──────────────────┐   │
│  │  Server  │───▶│ Embedded Agent   │   │
│  │  (UI +   │    │ (compose.RunUp)  │   │
│  │   API)   │    └──────────────────┘   │
│  └──────────┘              │            │
└───────────────────────────┼────────────┘
                             ▼
                     Docker socket
                   (docker compose up)
```

## Files

| File | Description |
|------|-------------|
| `docker-compose.yml` | Server compose file |
| `Dockerfile` | Multi-stage server image |
| `.env.example` | Environment variable template |

## Quick start

```bash
# 1. Copy env template
cp .env.example .env

# 2. Edit .env — set SECRET_KEY and APP_URL at minimum
nano .env

# 3. Build and start
docker compose up -d

# 4. Open the UI
open http://localhost:8090
```

## Environment variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `SECRET_KEY` | ✅ | — | 32-char random string for encrypting secrets |
| `APP_URL` | — | `http://localhost:8090` | Public URL of the server (used for mTLS and webhooks) |
| `PORT` | — | `8090` | Host port to expose |
| `PB_DATA_DIR` | — | `/data/pb_data` | PocketBase data directory |
| `REPOS_WORKSPACE` | — | `/data/repos` | Where git repositories are cloned |
| `WIREOPS_DISABLE_LOCAL_AGENT` | — | `false` | Set to `true` to disable the embedded agent |

## Disabling the embedded agent

If you later add remote agents and want to stop the embedded one from running:

```bash
# In .env
WIREOPS_DISABLE_LOCAL_AGENT=true
```

> [!NOTE]
> The embedded agent record will remain in the database with status `REVOKED`.
> Any stacks assigned to it will need to be re-assigned to a remote agent.
