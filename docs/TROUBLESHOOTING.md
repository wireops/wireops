# Troubleshooting

Start with the exact server and worker versions, the failing resource ID, and
the deploy timeline. Never paste `SECRET_KEY`, worker tokens, registry
credentials, repository keys, raw secret values, or unredacted backup files
into an issue.

## Common symptoms

| Symptom | Likely cause | Check |
|---|---|---|
| Server exits immediately | Missing/invalid `SECRET_KEY`, wrong data ownership, or migration failure | Server logs; `ls -ldn example/data` |
| `SECRET_KEY does not match this DATA_DIR` | Restored data was encrypted with a different key | Recovery vault entry and restore source |
| Worker stays offline | Wrong URL/token, TLS error, firewall, or version mismatch | Worker logs and `SERVER_URL` |
| Worker reports Docker permission denied | Container lacks the socket's group | `stat -c '%g' /var/run/docker.sock`; `DOCKER_GID` |
| Stack remains `pending` | Assigned worker is disconnected or command is queued | Worker status and pending deploy timeline |
| Stack creation/deploy is rejected | Compose lint error or worker policy violation | Review step and lint/policy phase |
| Secret resolution fails repeatedly | Provider disabled, path/field invalid, credentials expired, or wrong key | Integration **Test connection** and stack secret error |
| Private image will not pull | Credential not assigned, registry test failed, or daemon lacks insecure-registry configuration | Registry credential and worker daemon config |
| Backup is not mirrored | S3 integration/configuration, permissions, KMS, or network | Backup audit entry and server logs |
| UI loads but requests fail | Incorrect `APP_URL`, reverse-proxy headers, CORS, or stale frontend cache | Browser network panel and server origin |

## Server filesystem permissions

The container runs as UID/GID `1000`. On Linux:

```bash
cd example
ls -ldn data
sudo chown -R 1000:1000 data
chmod 700 data
docker compose restart wireops
```

Stop and inspect before changing ownership if the directory is shared with
another service. Do not apply recursive permission changes to `/`, a home
directory, or the Docker data root.

## Docker socket permissions

Inspect, do not weaken, the socket:

```bash
ls -ln /var/run/docker.sock
stat -c '%g' /var/run/docker.sock
```

Set that numeric value as `DOCKER_GID` and recreate the worker. Do not solve the
problem with `chmod 666 /var/run/docker.sock`.

## Worker connection and TLS

Confirm the URL scheme matches the server mode:

- `TLS_ENABLED=false`: `http://server:8443`
- trusted/native TLS: `https://server.example.com:8443`
- self-signed TLS in a controlled environment: `https://...` plus
  `WORKER_TLS_SKIP_VERIFY=true`

`WORKER_TOKEN` is shown when issued and should be stored on the worker. If it
was lost or revoked, create a new token rather than attempting to recover it
from logs. Keep server and worker on the exact same release.

## Stuck or failed deploys

1. Open the deploy timeline and identify the first failed phase.
2. Check whether the worker is `ACTIVE` and whether the command was acknowledged.
3. Read lint and policy findings before looking at Docker output.
4. Confirm the repository commit and rendered compose preview are the intended
   ones.
5. Check Docker/Compose output on the worker for pull, port, volume, and health
   failures.
6. Retry on a non-critical stack only after the underlying cause is fixed.

Avoid force redeploy as a generic retry button: it may recreate containers,
volumes, or networks according to the selected options.

## Backup and restore failures

- A backup archive must be a valid `.zip` accepted by the upload endpoint.
- Remote-only backups are downloaded locally before restore.
- Encrypted S3 objects require the original `SECRET_KEY` or the matching KMS
  access, according to the metadata stored with that object.
- Restore the backup with the same wireops version that created it, then
  upgrade through the documented path.
- The built-in archive does not contain the default `/data/repos` and
  `/data/stacks` siblings.

See [`DISASTER_RECOVERY.md`](DISASTER_RECOVERY.md) before attempting recovery.

## Useful health checks

```bash
curl --fail http://127.0.0.1:8090/api/health
docker compose ps
docker compose logs --tail=200 wireops
docker compose logs --tail=200 wireops-worker
```

Redact hostnames, repository URLs, tokens, environment values, request headers,
and filesystem paths that reveal private infrastructure before sharing output.
