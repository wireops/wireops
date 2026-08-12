# Changelog

User-visible changes are recorded here. The project follows semantic versioning
from `v1.0.0`; pre-1.0 releases may include breaking configuration or protocol
changes in a minor release.

## Unreleased

### Added

- Per-service **Restart All** and **Stop All** controls, plus multi-select
  restart/stop operations for arbitrary containers in a stack.

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
