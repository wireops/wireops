# wireops — Deployment Examples

This directory contains ready-to-use Docker deployment setups for each supported architecture.

---

## [1. server-embedded](./server-embedded/)

**Server with the built-in embedded agent.**

Everything in a single container. The simplest way to get started.

```bash
cd server-embedded
cp .env.example .env && nano .env
docker compose up -d
```

---

## [2. agent](./agent/)

**Standalone remote agent.**

Run a wireops agent on a remote host that connects to a central server.  
Requires a running wireops server and a bootstrap token from the admin panel.

```bash
cd agent
cp .env.example .env && nano .env   # set WIREOPS_SERVER, WIREOPS_MTLS_SERVER, WIREOPS_BOOTSTRAP_TOKEN
docker compose up -d
```

---

## [3. server-and-agent](./server-and-agent/)

**Server + dedicated agent on the same host.**

The embedded agent is disabled. A separate agent container connects to the server over mTLS and handles all `docker compose` execution. This gives you the cleanest architectural separation.

```bash
cd server-and-agent
cp .env.example .env && nano .env   # set SECRET_KEY
# First: start the server only, create a bootstrap token in the UI
docker compose up -d wireops
# Then: set WIREOPS_BOOTSTRAP_TOKEN in .env and start the agent
docker compose up -d wireops-agent
```

---

## Choosing a deployment

| | server-embedded | agent | server-and-agent |
|---|---|---|---|
| Single host | ✅ | ❌ | ✅ |
| Multiple hosts | ❌ | ✅ | ✅ |
| Clean separation | ❌ | ✅ | ✅ |
| Simplest setup | ✅ | — | — |
| Remote agents supported later | ✅ | — | ✅ |
