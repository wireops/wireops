# Changelog

User-visible changes are recorded here. The project follows semantic versioning
from `v1.0.0`; pre-1.0 releases may include breaking configuration or protocol
changes in a minor release.

## Unreleased

### Added

- The MCP server now exposes the `operator` actions, not only reads: deploy,
  force redeploy, rollback, pause/resume, render-time overrides, container
  restart/stop, transfer, delete, env var writes, stack import, stack creation
  from a repository compose file, repository and scheduled-job management, job
  runs, worker revocation, and one-shot `exec_in_container`, which reports the
  command's output and its exit code. Authorization is
  unchanged and still the server's: an API key belongs to a service account,
  which can only be `viewer` or `operator`, so a viewer-scoped key keeps
  getting `403` for every one of them and an instance that does not run the MCP
  container is unaffected.
- Every MCP tool now carries the protocol's read-only/destructive/idempotent
  hints, so a client can tell which calls are safe unattended, and the
  irreversible ones require a `confirm` field repeating the resource's exact
  name (stack name, worker hostname, repository name, job name) before any
  write leaves the process.
- Per-service **Restart All** and **Stop All** controls, plus multi-select
  restart/stop operations for arbitrary containers in a stack.

### Security

- Rebuild the server, worker, and MCP images on Alpine 3.24; the previous
  Alpine 3.20 base reached end of support in April 2026 and no longer received
  security updates.
- Update the Docker CLI embedded in the server and worker images to 29.x.
- Fail the `govulncheck` CI gate when the scanner itself cannot run (crash or
  unreachable vulnerability database) instead of reporting a clean result.
- Require Go 1.27.2 as the minimum toolchain in `go.mod`. CI and the release
  images already built on 1.27.2, but the module only asked for 1.27, so a
  local build could silently link the 1.27.0 standard library and its
  `crypto/tls`, `net/http` and `os` advisories.

### Changed

- Build with Go 1.27.2 and update Go and frontend dependencies, including
  PocketBase v0.40.

### Documentation

- Define the pre-1.0 server-worker compatibility policy.
- Add production, upgrade, troubleshooting, disaster-recovery, and security
  guidance.
- Pin the example server and worker images to one configurable release.
- Document Linux data-directory and Docker-socket permissions.

## v0.2.11 — 2026-08-11

### Added

- Worker-authenticated private registry pulls with encrypted registry
  credentials scoped to a stack deploy.
- Registry connection testing and explicit insecure-registry opt-in.
- Dashboard panel and quick-action improvements.

### Security

- Block server-side request forgery, redirect, and DNS-rebinding paths in
  registry connection tests.
- Require modern TLS for the worker registry-authentication preflight unless an
  operator explicitly enables insecure registry behavior.

Full release notes: <https://github.com/wireops/wireops/releases/tag/v0.2.11>

## Earlier releases

Release notes before `v0.2.11` remain available at
<https://github.com/wireops/wireops/releases>. Material upgrade notes will be
backfilled here when they affect the `v1.0` migration path.
