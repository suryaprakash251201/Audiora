# Audiora 2.0 — development helpers.
#
# Docker is the intended way to run this; these targets are for working on
# the code itself, where a hot-reloading frontend and a fast Go test loop
# matter more than TLS.

SHELL := /bin/bash
.DEFAULT_GOAL := help

# Where the dev server expects the API to be running.
export AUDIORA_API ?= http://localhost:8080

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

# --- frontend ---

.PHONY: install
install: ## Install frontend dependencies
	cd web && npm install

.PHONY: dev
dev: ## Run the Vite dev server with API proxying
	cd web && npm run dev

.PHONY: build-web
build-web: ## Typecheck and build the production frontend bundle
	cd web && npm run build

.PHONY: test-web
test-web: ## Run the frontend unit tests
	cd web && npm run test

.PHONY: typecheck
typecheck: ## Typecheck the frontend without emitting
	cd web && npm run typecheck

# --- backend ---

.PHONY: build-server
build-server: ## Build the Go server binary
	cd server && CGO_ENABLED=0 go build -trimpath -o audiora ./cmd/audiora

.PHONY: test-server
test-server: ## Run the Go test suite
	cd server && go test ./...

.PHONY: test-race
test-race: ## Run the Go test suite under the race detector
	cd server && go test -race ./...

.PHONY: vet
vet: ## Vet and format-check the Go code
	cd server && go vet ./... && gofmt -l .

.PHONY: cover
cover: ## Run the Go tests with a coverage summary
	cd server && go test -cover ./...

# --- everything ---

.PHONY: test
test: test-server test-web ## Run every test suite

.PHONY: check
check: vet typecheck test ## Everything CI would run

# --- containers ---

.PHONY: up
up: ## Build and start the full stack
	docker compose up -d --build

.PHONY: down
down: ## Stop the stack, keeping all data
	docker compose down

.PHONY: nuke
nuke: ## Stop the stack and DELETE the database, cover art and transcode cache
	@read -p "This deletes the Audiora database, cover art and transcode cache. Continue? [y/N] " a; \
	[[ $$a == "y" ]] && docker compose down -v || echo "Cancelled."

.PHONY: logs
logs: ## Follow the logs
	docker compose logs -f

.PHONY: config
config: ## Print the resolved compose configuration
	docker compose config

# --- mobile ---

.PHONY: cap-sync
cap-sync: ## Build the web app and copy it into the native projects
	cd web && npm run cap:sync

.PHONY: cap-add
cap-add: ## Add the Android and iOS native projects (run once)
	cd web && npx cap add android && npx cap add ios

.PHONY: cap-android
cap-android: ## Open the Android project in Android Studio
	cd web && npm run cap:android

.PHONY: cap-ios
cap-ios: ## Open the iOS project in Xcode
	cd web && npm run cap:ios
