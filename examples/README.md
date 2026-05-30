# wireops — Deployment Examples

This directory contains ready-to-use Docker deployment setups for the supported remote-worker architectures.

---

## [1. worker](./worker/)

**Standalone remote worker.**

Run a wireops worker on a remote host that connects to a central server.
Requires a running wireops server and a worker token from the admin panel.

```bash
cd worker
cp .env.example .env && nano .env   # set SERVER_URL and WORKER_TOKEN
docker compose up -d
```

---

## [2. server-and-worker](./server-and-worker/)

**Server + dedicated worker on the same host.**

A separate worker container connects to the server and handles all `docker compose` execution.

```bash
cd server-and-worker
cp .env.example .env && nano .env   # set SECRET_KEY
# First: start the server only, create a worker token in the UI
docker compose up -d wireops
# Then: set WORKER_TOKEN in .env and start the worker
docker compose up -d wireops-worker
```

---

## Choosing a deployment

| | worker | server-and-worker |
|---|---|---|
| Single host | ❌ | ✅ |
| Multiple hosts | ✅ | ✅ |
| Clean separation | ✅ | ✅ |
| Simplest setup | — | ✅ |
