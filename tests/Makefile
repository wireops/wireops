.PHONY: vault vault-down vault-restart vault-logs vault-seed vault-clean vault-info \
        infisical infisical-down infisical-restart infisical-logs infisical-clean infisical-info

# --- Vault ---

vault:
	docker compose -f vault/docker-compose.yml up -d
	@$(MAKE) --no-print-directory vault-info

vault-info:
	@echo ""
	@echo "== Vault =="
	@echo "  URL:         http://localhost:8200"
	@echo "  Root token:  $${VAULT_DEV_ROOT_TOKEN_ID:-root}"
	@echo "  KV v2 mount: secret/  (already enabled by dev mode)"
	@echo "  Seed a test secret:  make vault-seed"
	@echo ""

vault-down:
	docker compose -f vault/docker-compose.yml down

vault-restart: vault-down vault

vault-logs:
	docker compose -f vault/docker-compose.yml logs -f

# Seeds a test secret at secret/myapp#DB_PASS for the wireops Vault provider.
vault-seed:
	docker exec -e VAULT_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN=$${VAULT_DEV_ROOT_TOKEN_ID:-root} \
		wireops-test-vault vault kv put secret/myapp DB_PASS=s3cr3t
	@echo "Seeded secret/myapp#DB_PASS = s3cr3t"

vault-clean: vault-down
	rm -rf vault/.data

# --- Infisical ---

# Generated once; re-run `make infisical-clean` to force fresh secrets.
infisical/.env:
	@echo "Generating infisical/.env with random ENCRYPTION_KEY/AUTH_SECRET..."
	@{ \
		echo "INFISICAL_ENCRYPTION_KEY=$$(openssl rand -hex 16)"; \
		echo "INFISICAL_AUTH_SECRET=$$(openssl rand -base64 32)"; \
	} > infisical/.env

infisical: infisical/.env
	docker compose -f infisical/docker-compose.yml --env-file infisical/.env up -d
	@$(MAKE) --no-print-directory infisical-info

infisical-info:
	@echo ""
	@echo "== Infisical =="
	@echo "  URL:          http://localhost:9900"
	@echo "  Secrets file: infisical/.env"
	@echo "  First boot needs a bit to migrate — watch: make infisical-logs"
	@echo "  Then: sign up, create a project, add a Universal Auth machine identity"
	@echo "  (see tests/README.md for the exact steps and where they map in wireops)"
	@echo ""

infisical-down:
	docker compose -f infisical/docker-compose.yml --env-file infisical/.env down

infisical-restart: infisical-down infisical

infisical-logs:
	docker compose -f infisical/docker-compose.yml --env-file infisical/.env logs -f

infisical-clean: infisical-down
	rm -rf infisical/.data infisical/.env
