# Security policy

wireops stores credentials and can execute Docker operations on connected
hosts. Treat security reports as sensitive even when the initial impact is
uncertain.

## Supported versions

The supported release line is `v1.0.x`; security fixes target its latest
released patch. Older releases may not receive security fixes.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting for this repository (**Security →
Report a vulnerability**). Include:

- affected version/commit and component (server, worker, MCP, or frontend);
- deployment assumptions required to reproduce it;
- minimal reproduction steps or a proof of concept;
- observed and expected behavior;
- potential impact and any known mitigations.

Do not open a public issue containing exploit details, credentials, tokens,
private URLs, customer data, or backup archives. If private reporting is not
available, contact the repository owner privately through the contact method on
their GitHub profile and disclose only enough to establish a secure channel.

Reports are handled on a best-effort basis; this hobby project does not promise
an SLA. The maintainer will acknowledge receipt, validate impact, coordinate a
fix and release, and credit the reporter when requested and appropriate.

## Security boundaries

- A worker with access to the Docker socket has root-equivalent control of its
  host. Worker policy reduces accidental/unauthorized workload capabilities;
  it is not a sandbox against a fully compromised worker host.
- `SECRET_KEY` protects encrypted values at rest but does not protect a running
  server whose process or host is compromised.
- Workers keep existing containers running when the server is unavailable;
  wireops is not a high-availability workload orchestrator.
- Features explicitly outside the project scope, unsupported mixed-version
  deployments, and deployments that make the Docker socket publicly reachable
  are not security guarantees made by wireops.

## Operator responsibilities

Pin releases, use TLS, restrict network exposure, protect and back up
`SECRET_KEY` separately, use least-privileged RBAC, configure worker policy,
keep Docker and the host patched, and test restores. See
[the production checklist](https://wireops.dev/docs/operations/production/).
