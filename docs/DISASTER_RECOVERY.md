# Disaster Recovery

Covers losing the server host (DB, secrets, stack files). Losing the server
does **not** mean losing your workers — they keep running your stacks'
containers on their own while the server is down.

All backup management lives in **Settings → Backups**.

## The one rule that matters

**`SECRET_KEY` must survive separately from your backups.** It decrypts
every stored secret (git passwords, SSH keys, integration tokens). Lose it,
and a backup is just an encrypted brick — the data's there, permanently
unreadable.

- Keep `SECRET_KEY` in a password manager / vault, never next to the backup
  file itself.
- If it doesn't match on restore, wireops refuses to start and tells you
  clearly — it won't silently corrupt anything.

## Day to day

- Backups: manual (on demand) or scheduled (cron), with history/download/
  delete in the UI.
- Storage: local disk, or S3-compatible (AWS S3, R2, MinIO, B2, ...) —
  off-host storage is strongly recommended.
- Retention: cap how many scheduled backups to keep.
- Restore: replaces **all** server data and restarts the process. Rolls
  back automatically if it fails mid-way.

### Enabling S3

Settings → Backups → fill in:

| Field | Value |
|---|---|
| Endpoint | provider URL, e.g. `https://s3.us-east-1.amazonaws.com` |
| Bucket | must already exist — wireops won't create one |
| Region | e.g. `us-east-1` |
| Access Key / Secret Key | scoped to that one bucket only, read+write+list+delete |
| Force path-style | on for MinIO / most self-hosted S3 |

Once set, every backup goes to S3 automatically. On a new server, point S3
settings at the *same bucket* before restoring — restore reads from whatever
storage is currently configured, not from where the backup was made.

### Restoring an uploaded file

Upload is locked to a real PocketBase superuser (not a wireops admin role)
— accepting an arbitrary file as a future full-restore target needs the
extra bar. With superuser creds: "Upload Backup" button in Settings →
Backups, or send the file with a superuser token straight to the upload
endpoint. No superuser? Drop the file into the server's backups folder (or
the configured S3 bucket) directly — it'll show up in the UI like any other
backup.

**No API/UI access at all** (server down, locked out, etc.): copy the
backup `.zip` straight onto the host's disk, into `<DATA_DIR>/backups/`
(default `pb_data/backups/`) — wireops creates this folder automatically on
boot. Once the file lands
there, it appears in Settings → Backups like a normal backup and can be
restored from the UI. This bypasses the upload endpoint's superuser gate
entirely, since it requires filesystem access to the host — the same trust
level as SSH access to the server.

## Recovering onto a new server — checklist

1. Same wireops version.
2. `SECRET_KEY` from your vault (never from a backup).
3. Same S3 settings as the original server, if used.
4. Start it. Wrong `SECRET_KEY` → refuses to boot with an error. **Fix that
   before anything else.**
5. Settings → Backups → pick the backup → restore.
6. Verify: login works, stacks show up, a known secret decrypts, sync
   resumes clean.
7. Workers reconnect on their own. Any worker whose token wasn't backed up
   separately needs a new one.

## Worker recovery

Not yet built — no automated flow for recovering a lost worker node,
replacing a missing agent container, or adopting orphaned stacks. Track
this in the roadmap.
