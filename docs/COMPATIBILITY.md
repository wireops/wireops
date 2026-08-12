# Compatibility and support

This page defines what an operator can rely on when combining wireops
components and runtime dependencies. It is intentionally conservative: the
server and worker exchange a shared, evolving WebSocket protocol, and the
server does not yet reject an incompatible worker during registration.

## Server and worker versions

Until an explicit protocol negotiation mechanism ships, the supported
combination is:

| Server | Worker | Support |
|---|---|---|
| `v0.2.x` | Exact same release | Supported |
| `v0.2.x` | Different patch/minor release | Not guaranteed |
| Development build | Same commit | Development only |

For example, pair server `v0.2.11` with worker `v0.2.11`. Update all workers
immediately after the server and do not rely on a mixed-version fleet. A
worker may appear connected while only failing later when it receives a
message introduced by a newer server.

`v1.0` will not retroactively make old pre-1.0 workers compatible. A formal
multi-version window will be documented here only after it is enforced by the
registration/handshake path and covered by tests.

## Runtime support

Release images are built for Linux `amd64` and `arm64`. The worker requires:

- a reachable Docker Engine;
- the Docker Compose v2 CLI plugin (`docker compose`, not legacy
  `docker-compose`);
- permission to use the Docker socket or the configured Docker endpoint;
- outbound HTTP(S)/WebSocket access to the wireops server.

No minimum Docker Engine or Compose version is currently enforced. Before
production use, validate the exact host combination with:

```bash
docker version
docker compose version
```

Docker Desktop may work for development, but the production deployment model
is a Linux host. Rootless Docker and non-default socket paths require adjusting
the worker mount and permissions and are not part of the default example.

## Browser support

The frontend is built as a modern static SPA. There is no formal legacy-browser
matrix. Use a currently supported release of Chrome/Chromium, Firefox, Safari,
or Edge. File an issue with the browser name and exact version when reporting a
UI compatibility problem.

## External services

Vault, Infisical, S3-compatible storage, Git providers, reverse proxies, and
notification services evolve independently. A provider being API-compatible
does not mean every product/version has been tested. Keep a staging stack for
provider upgrades and use each integration's **Test connection** action before
depending on it.

## Support policy

While the project is pre-1.0, only the latest released patch is expected to
receive fixes. Security support is defined in [`SECURITY.md`](../SECURITY.md).
Upgrade instructions live in [`UPGRADING.md`](UPGRADING.md).
