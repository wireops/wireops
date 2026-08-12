# Upgrading wireops

The server applies PocketBase schema migrations on startup. Treat an upgrade as
a data migration, not just an image restart. Downgrades are unsupported unless
you restore a backup created before the upgrade.

## Before every upgrade

1. Read [`CHANGELOG.md`](../CHANGELOG.md) and the GitHub release notes for every
   version being crossed.
2. Confirm the target server and worker versions match; see
   [`COMPATIBILITY.md`](COMPATIBILITY.md).
3. Create a manual backup in **Settings → Backups** and wait for its S3 mirror,
   if configured, to complete.
4. Copy `SECRET_KEY` from the runtime into the password manager/vault entry used
   for recovery. Never put it inside the backup archive or in Git.
5. Record the current image version and confirm the current instance is healthy.

For a high-value installation, also snapshot the complete `DATA_DIR`. The
built-in archive protects PocketBase data but not the default sibling
directories used for repository clones and rendered stack artifacts; see
[`DISASTER_RECOVERY.md`](DISASTER_RECOVERY.md).

## Compose deployment

The example deployment uses one `WIREOPS_VERSION` value for both images. Change
it to the target release, pull first, then replace the server:

```bash
cd example
docker compose pull wireops
docker compose up -d wireops
docker compose ps
docker compose logs --tail=200 wireops
```

Wait for the server health check and migrations to finish. Confirm you can log
in, workers are listed, and the API health endpoint responds before updating
workers:

```bash
curl --fail http://127.0.0.1:8090/api/health
docker compose pull wireops-worker
docker compose up -d wireops-worker
docker compose ps
```

For remote workers, update each host to the exact same `WIREOPS_VERSION`. Keep
the mixed-version period as short as possible. Verify every worker returns to
`ACTIVE` before triggering deployments.

## Post-upgrade checks

- Server and every worker report the intended version.
- A read-only stack status/compose request succeeds.
- One non-critical stack can sync and completes its post-deploy check.
- One scheduled job can run manually and reports its exit status.
- Secret-backed environment variables still resolve.
- Metrics and configured notifications still arrive.
- Backup listing and the most recent off-host mirror remain visible.

Do not test a production upgrade for the first time on the most critical stack.

## Rollback

Restarting an older image against a database already migrated by a newer image
is not a supported rollback. If an upgrade must be reversed:

1. Stop the server and workers from accepting new changes.
2. Restore the pre-upgrade backup using the version that created it.
3. Restore the separately protected `SECRET_KEY` used by that data.
4. Start the old server version.
5. Start matching old worker versions and perform the post-upgrade checks above.

If the server cannot start, copy the backup archive into
`<PB_DATA_DIR>/backups/` and follow the offline recovery path in
[`DISASTER_RECOVERY.md`](DISASTER_RECOVERY.md).

## Upgrade evidence for `v1.0`

Before promoting a release candidate to `v1.0`, record a successful drill of:

- clean install on Linux `amd64` and `arm64`;
- upgrade from the latest `v0.2.x` release with populated data;
- server/worker reconnect;
- deploy, scheduled job, rollback, backup, and restore;
- recovery with the original `SECRET_KEY` on a fresh host.
