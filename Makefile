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
	@if [ "$$START_WORKER" = "true" ] || [ -n "$$WIREOPS_BOOTSTRAP_TOKEN" ]; then \
		echo "  wireops Worker:    Enabled"; \
	fi
	@echo "========================================"
	@trap 'kill 0' SIGINT; \
	go run . serve --http=0.0.0.0:8090 & \
	sleep 2 && cd $(FRONTEND_DIR) && npm run dev & \
	if [ "$$START_WORKER" = "true" ] || [ -n "$$WIREOPS_BOOTSTRAP_TOKEN" ]; then \
		export WIREOPS_SERVER="$${WIREOPS_SERVER:-http://localhost:8090}"; \
		export WIREOPS_MTLS_SERVER="$${WIREOPS_MTLS_SERVER:-https://localhost:8443}"; \
		export WIREOPS_WORKER_PKI_DIR="$${WIREOPS_WORKER_PKI_DIR:-./worker_pki}"; \
		echo "[Dev] Waiting 4s for server to start before launching worker..."; \
		sleep 4 && go run ./worker/main.go & \
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

docker-build-worker:
	docker build -f Dockerfile.worker -t wireops-worker:latest .

docker-run-worker:
	docker compose -f examples/worker/docker-compose.yml --env-file examples/worker/.env up -d

docker-run-all:
	docker compose -f examples/server-and-worker/docker-compose.yml --env-file examples/server-and-worker/.env up -d
