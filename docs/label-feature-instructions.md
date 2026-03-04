# wireops – Label System Implementation Plan (Server-Only Phase)

> Scope: This document defines the **label system architecture and implementation plan**
> for wireops **without agent implementation**.
>
> The server is responsible for:
> - Rendering compose files
> - Injecting labels
> - Versioning
> - Persisting revisions
> - Exposing desired state

Agents are **out of scope for this phase**.

---

# 1. Objective

Implement a deterministic, versioned label system that:

- Defines stack ownership
- Enables future drift detection
- Enables future orphan detection
- Supports reconciliation semantics
- Guarantees deterministic compose rendering
- Does not mutate the original repository files

---

# 2. Core Principles

1. Original `docker-compose.yml` is immutable.
2. Labels are injected during render time.
3. Labels are version-bound and immutable per revision.
4. A rendered compose is a snapshot of desired state.
5. Every revision must be reproducible from stored metadata.

---

# 3. Label Schema (v1)

Every service in a managed stack must include:

```yaml
labels:
  wireops.managed: "true"
  wireops.stack_id: "<uuid>"
  wireops.stack_name: "<string>"
  wireops.version: "<int>"
  wireops.commit: "<git_sha>"
  wireops.checksum: "<sha256>"
  wireops.generated_at: "<iso8601>"
4. Label Semantics
Label	Responsibility
wireops.managed	Ownership flag
wireops.stack_id	Immutable global identifier
wireops.stack_name	Human-readable name
wireops.version	Monotonic revision number
wireops.commit	Git reference used
wireops.checksum	Hash of rendered compose
wireops.generated_at	Audit trace
5. Server Rendering Pipeline
Step 1 – Repository Resolution

Clone repository

Checkout selected branch/ref

Read compose file path

Failure → abort revision generation.

Step 2 – Compose Parsing

Parse YAML safely

Validate presence of services

Ensure deterministic key ordering

Reject if:

Invalid YAML

No services

Unsupported schema

Step 3 – Label Injection Strategy

Never mutate original file.

Generate an override structure:

services:
  service_name:
    labels:
      wireops.managed: "true"
      ...

Injection rules:

If service has no labels → create labels block

If service has labels → merge without overwriting user labels

Never override non-wireops labels

Step 4 – Deterministic Merge

Final rendered compose is:

original compose
+ override structure

Merge must:

Preserve user definitions

Preserve order deterministically

Avoid duplicate keys

Step 5 – Checksum Calculation

Checksum must be computed on:

Fully rendered compose

Normalized YAML string

Deterministic key ordering

Example pseudo:

normalized_yaml = serialize(sorted_keys(rendered_compose))
checksum = sha256(normalized_yaml)

This checksum becomes part of labels.

6. Versioning Model

Each stack contains:

{
  "stack_id": "uuid",
  "current_version": 5,
  "desired_commit": "abc123",
  "checksum": "sha256..."
}
Version Increment Rules

Increment version when:

Git commit changes

Manual rollback selected

Structural configuration changed

Do NOT increment when:

Polling occurs without change

Server restarts

Metadata updates only

7. Revision Storage Model

Each revision must persist:

{
  "version": 5,
  "commit": "abc123",
  "checksum": "sha256...",
  "rendered_compose_path": "/storage/stacks/<id>/v5.yml",
  "created_at": "timestamp"
}

Requirements:

Immutable once created

Never overwritten

Stored on disk

Referenced by database

8. Rollback Logic

Rollback must:

Select previous revision

Create new version entry

Reuse stored rendered compose

Assign new version number

Preserve deterministic checksum

Rollback never edits history.

Rollback is a forward-moving action.

9. Deterministic Guarantees

The system must guarantee:

Same Git commit → same rendered output

Same rendered output → same checksum

Same checksum → same version (if not incremented)

This enables future reconciliation.

10. Error Handling Strategy

If rendering fails:

Do not increment version

Do not persist revision

Store failure event

Surface error to user

System must never enter partial revision state.

11. Observability Model (Server-Side)

Track per stack:

Current version

Last successful render time

Last commit processed

Last render status

Revision history

12. Storage Layout (Example)
/var/lib/wireops/
  stacks/
    <stack_id>/
      v1.yml
      v2.yml
      v3.yml

Database stores metadata only.
Rendered compose files stored on disk.

13. Edge Cases to Handle

Services with pre-existing labels

Multiple services

External networks

Named volumes

Empty label blocks

Compose v2 and v3 formats

Large compose files

Invalid Git refs

14. Testing Requirements
Unit Tests

Label injection correctness

Deterministic checksum generation

Version increment logic

Rollback generation

Integration Tests

Same commit twice → no version bump

Different commit → version bump

Rollback produces new version

Rendering large compose file

Label merge does not overwrite user labels

15. Completion Criteria (Server Phase)

Label system is complete when:

All rendered services include wireops labels

Checksum is deterministic

Version increments correctly

Rollback works

Revision history is immutable

No mutation of original repository files

Re-rendering same commit produces identical checksum

16. Strategic Outcome

After this phase:

The server becomes a deterministic desired-state generator.

Every stack has versioned, labeled snapshots.

The system is ready for agent reconciliation in a future phase.

Labels become the canonical ownership boundary.

This concludes the server-only label system design.