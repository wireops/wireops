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
	@if [ "$$START_WORKER" = "true" ] || [ -n "$$WORKER_TOKEN" ]; then \
		echo "  wireops Worker:    Enabled"; \
	fi
	@echo "========================================"
	@backend_pid=""; \
	frontend_pid=""; \
	worker_pid=""; \
	stop_pid() { \
		pid="$$1"; \
		sig="$$2"; \
		if [ -n "$$pid" ] && kill -0 "$$pid" 2>/dev/null; then \
			kill -s "$$sig" "$$pid" 2>/dev/null || true; \
		fi; \
	}; \
	cleanup() { \
		trap - INT TERM EXIT; \
		stop_pid "$$worker_pid" INT; \
		stop_pid "$$frontend_pid" INT; \
		stop_pid "$$backend_pid" INT; \
		sleep 1; \
		stop_pid "$$worker_pid" TERM; \
		stop_pid "$$frontend_pid" TERM; \
		stop_pid "$$backend_pid" TERM; \
		wait $$worker_pid $$frontend_pid $$backend_pid 2>/dev/null || true; \
	}; \
	trap 'cleanup; exit 130' INT TERM; \
	trap 'cleanup' EXIT; \
	go run . serve --http=0.0.0.0:8090 >/dev/stdout 2>/dev/stderr & \
	backend_pid=$$!; \
	sleep 2; \
	sh -c 'cd $(FRONTEND_DIR) && exec npm run dev' >/dev/stdout 2>/dev/stderr & \
	frontend_pid=$$!; \
	if [ "$$START_WORKER" = "true" ] || [ -n "$$WORKER_TOKEN" ]; then \
		export SERVER_URL="$${SERVER_URL:-http://localhost:8443}"; \
		echo "[Dev] Waiting 4s for server to start before launching worker..."; \
		sleep 4; \
		go run ./worker/main.go >/dev/stdout 2>/dev/stderr & \
		worker_pid=$$!; \
	fi; \
	wait $$backend_pid $$frontend_pid $$worker_pid

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

docker-build-mcp:
	docker build -f Dockerfile.mcp -t wireops-mcp:latest .

docker-run-mcp:
	docker compose -f examples/mcp/docker-compose.yml --env-file examples/mcp/.env up -d
