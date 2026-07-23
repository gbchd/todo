FRONTEND_DIR := internal/transport/web/frontend

.PHONY: build frontend frontend-dev test

# Two-step build: the frontend must be built before `go build`, since
# internal/transport/web embeds internal/transport/web/static at compile
# time (//go:embed static) and Vite's outDir writes straight into it.
build: frontend
	go build -o bin/todo ./cmd/todo

frontend:
	npm --prefix $(FRONTEND_DIR) ci
	npm --prefix $(FRONTEND_DIR) run build

# Hot-reload dev server (Vite on :5173, proxying /api to `todo serve` on
# :8080 — see frontend/vite.config.ts). Run `todo serve` yourself alongside
# this.
frontend-dev:
	npm --prefix $(FRONTEND_DIR) run dev

test:
	go test ./...
