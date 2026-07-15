# Manual test environments

Standalone `docker-compose` setups to spin up a local Vault or Infisical
instance for manually testing wireops' `vault`/`infisical` secret providers
(`internal/secrets/vault.go`, `internal/secrets/infisical.go`). Dev-only —
not for production use. One `Makefile` here in `tests/` drives both; each
service's compose file/data lives in its own subfolder (`vault/`,
`infisical/`), bind-mounted into a local `.data/` folder (gitignored)
instead of a Docker-managed volume. `make vault`/`make infisical` print the
URL and other connection info right after starting (re-print anytime with
`make vault-info` / `make infisical-info`).

## Vault

```bash
cd tests
make vault          # start
make vault-seed      # writes secret/myapp#DB_PASS = s3cr3t
make vault-logs       # follow logs
make vault-down      # stop
make vault-clean     # stop + wipe vault/.data
```

Dev mode: unsealed, in-memory, root token defaults to `root` (override with
`VAULT_DEV_ROOT_TOKEN_ID`). A KV v2 engine is already mounted at `secret/`.
Dev mode has no real storage backend, so `.data` here only catches an audit
log if you enable one — nothing to restore across restarts.

In wireops → Settings → Integrations → HashiCorp Vault:
- Address: `http://localhost:8200`
- Token: `root`

Env var reference: `secret/data/myapp#DB_PASS`

## Infisical

```bash
cd tests
make infisical       # generates infisical/.env (random secrets) then starts
make infisical-logs   # follow logs (first boot takes a bit to migrate)
make infisical-down  # stop
make infisical-clean # stop + wipe infisical/.data and infisical/.env
```

`make infisical` generates `infisical/.env` with a random `ENCRYPTION_KEY`
(hex) and `AUTH_SECRET` (base64) on first run — the compose file requires
both and fails fast if they're missing, so it never silently falls back to a
shared/hardcoded secret. Re-running `make infisical` reuses the same `.env`;
run `make infisical-clean` first to force fresh secrets.

All-in-one image (bundled Postgres, persisted to `infisical/.data`).

1. Open `http://localhost:9900`, complete the signup wizard, create a project.
2. Project Settings → Machine Identities → create one, attach Universal Auth,
   note the generated **Client ID** and **Client Secret**.
3. Note the **Project ID** (Project Settings → General) and the environment
   slug you're using (e.g. `dev`).

In wireops → Settings → Integrations → Infisical:
- Site URL: `http://localhost:9900`
- Client ID / Client Secret: from step 2

Env var reference: `<project-id>/<environment-slug>#SECRET_NAME`
(add a `/path` segment before `#` if the secret isn't at the environment root)
