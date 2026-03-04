BINARY=wireops
FRONTEND_DIR=frontend
PB_PUBLIC=pb_public

.PHONY: all build build-frontend dev clean docker-build

all: build-frontend build

build:
	go build -o $(BINARY) .

build-frontend:
	cd $(FRONTEND_DIR) && npm install && npm run generate
	rm -rf $(PB_PUBLIC)
	cp -r $(FRONTEND_DIR)/.output/public $(PB_PUBLIC)

dev:
	@echo "========================================"
	@echo "  wireops Dev Mode"
	@echo "  PocketBase API:  http://localhost:8090"
	@echo "  PB Admin Panel:  http://localhost:8090/_/"
	@echo "  Nuxt Frontend:   http://localhost:3000"
	@if [ "$$START_AGENT" = "true" ] || [ -n "$$WIREOPS_BOOTSTRAP_TOKEN" ]; then \
		echo "  wireops Agent:     Enabled"; \
	fi
	@echo "========================================"
	@trap 'kill 0' SIGINT; \
	go run . serve --http=0.0.0.0:8090 & \
	sleep 2 && cd $(FRONTEND_DIR) && npm run dev & \
	if [ "$$START_AGENT" = "true" ] || [ -n "$$WIREOPS_BOOTSTRAP_TOKEN" ]; then \
		export WIREOPS_SERVER="$${WIREOPS_SERVER:-http://localhost:8090}"; \
		export WIREOPS_MTLS_SERVER="$${WIREOPS_MTLS_SERVER:-https://localhost:8443}"; \
		export WIREOPS_AGENT_PKI_DIR="$${WIREOPS_AGENT_PKI_DIR:-./agent_pki}"; \
		echo "[Dev] Waiting 4s for server to start before launching agent..."; \
		sleep 4 && go run ./agent/main.go & \
	fi; \
	wait

dev-backend:
	go run . serve --http=0.0.0.0:8090

dev-frontend:
	cd $(FRONTEND_DIR) && npm run dev

clean:
	rm -f $(BINARY)
	rm -rf $(PB_PUBLIC)
	rm -rf $(FRONTEND_DIR)/.output
	rm -rf $(FRONTEND_DIR)/.nuxt

docker-build:
	docker build -t wireops:latest .

docker-run:
	docker compose -f examples/server-embedded/docker-compose.yml up -d

docker-build-agent:
	docker build -f Dockerfile.agent -t wireops-agent:latest .

docker-run-agent:
	docker compose -f examples/agent/docker-compose.yml --env-file examples/agent/.env up -d

docker-run-all:
	docker compose -f examples/server-and-agent/docker-compose.yml --env-file examples/server-and-agent/.env up -d
