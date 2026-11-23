# ─────────────────────────────────────────────────────────────────────────────
#  IMS Go API Starter — developer commands
#
#  Run `make` (or `make help`) for the full list. Recipes use only portable
#  tools (go, gofmt, and optionally air / golangci-lint / docker), so they work
#  the same on Windows, macOS and Linux.
# ─────────────────────────────────────────────────────────────────────────────

APP                    := ims
MODULE                 := github.com/Innovix-Matrix-Systems/ims-go-api-starter
VERSION                ?= $(strip $(file < VERSION))

GOEXE                  := $(shell go env GOEXE)
BIN                    := bin/$(APP)$(GOEXE)
LDFLAGS                := -X $(MODULE)/internal/version.Version=$(VERSION)

SQLC_VERSION           ?= v1.31.1
OAPI_CODEGEN_VERSION   ?= v2.4.1
AIR_VERSION            ?= latest
GOLANGCI_VERSION       ?= v1.64.8

DOCKER_COMPOSE         := docker compose -f tests/docker-compose.yml

# Local machines can exhaust memory during parallel race builds; CI overrides
# this to run fully parallel (`make test TEST_FLAGS=`).
TEST_FLAGS             ?= -p 2

# Passed to `ims key:generate`: --show prints the key instead of writing .env,
# --force replaces a key already set (`make key-generate KEY_FLAGS=--show`).
KEY_FLAGS              ?=

# `rm -rf` does not exist in the Windows command shell.
ifeq ($(OS),Windows_NT)
  RM := rmdir /s /q
else
  RM := rm -rf
endif

.DEFAULT_GOAL := help
.PHONY: help tools rename env key-generate build build-all dev dev-api run serve queue schedule test test-race test-cover test-integration \
        vet format format-check lint lint-fix tidy check \
        sqlc openapi openapi-gen openapi-check generate gen-check skills-sync skills-check \
        db-up db-down migrate migrate-status migrate-rollback seed fresh \
        health config version web-build web-typecheck dev-web docs docs-build docs-gen clean clean-all

# ── Help ─────────────────────────────────────────────────────────────────────
help:
	@echo IMS Go API Starter - developer commands
	@echo --- Setup ---
	@echo   make tools             Install dev tools: air, golangci-lint
	@echo   make rename MODULE=... Rename the starter to your app, once after creating it
	@echo   make key-generate      Generate APP_KEY into .env once, KEY_FLAGS=--show prints it
	@echo --- Develop ---
	@echo   make dev               Run SPA :8080 with Vite HMR + API :8081 with air
	@echo   make dev-api           Run the API only with air
	@echo   make run               Run the API once with go run
	@echo   make queue             Run the queue worker with go run
	@echo   make schedule          Run the scheduler daemon with go run
	@echo   make build             Build the release binary into bin/
	@echo   make build-all         Build the SPA and then the Go binary
	@echo   make dev-web           Run the developer dashboard SPA on :5173
	@echo --- Quality ---
	@echo   make test              Unit tests, integration tests skip without a DSN
	@echo   make test-race         Unit tests with the race detector
	@echo   make test-cover        Unit tests and an HTML coverage report
	@echo   make test-integration  Full suite against Postgres, set DATABASE_TEST_DSN
	@echo   make lint              Run golangci-lint
	@echo   make lint-fix          Run golangci-lint with --fix
	@echo   make format            Format all Go code with gofmt
	@echo   make format-check      Fail if any file is not gofmt-clean
	@echo   make vet               Run go vet
	@echo   make check             format-check + vet + lint + test
	@echo --- Code generation ---
	@echo   make sqlc              Regenerate internal/dbgen from sql/ and migrations/
	@echo   make openapi           Regenerate internal/contract from api/openapi.yaml
	@echo   make openapi-gen       Refresh the generated regions of api/openapi.yaml
	@echo   make openapi-check     Validate the spec + route drift + region freshness
	@echo   make generate          sqlc + openapi + openapi-gen
	@echo   make gen-check         Fail if generated code or docs are out of date
	@echo --- Documentation ---
	@echo   make docs              Preview the docs site at :4321, needs Node 22.23+
	@echo   make docs-build        Build the docs site and check every link
	@echo   make docs-gen          Regenerate the CLI/config/permissions/make reference pages
	@echo --- Database ---
	@echo   make db-up             Start local Postgres via docker compose
	@echo   make db-down           Stop local Postgres
	@echo   make migrate           Apply pending migrations
	@echo   make migrate-status    Show migration status
	@echo   make migrate-rollback  Roll back the most recent migration
	@echo   make seed              Seed permissions, tenant/branch/roles, admin + developer
	@echo   make fresh             Roll back everything, migrate and seed - destructive
	@echo --- Operations ---
	@echo   make health            Run the health checks
	@echo   make config            Print the effective configuration
	@echo   make version           Print the build version
	@echo   make tidy              go mod tidy
	@echo   make clean             Remove build artifacts
	@echo   make clean-all         Remove build artifacts and local state in var/

# ── Setup ────────────────────────────────────────────────────────────────────
tools: ## Install the developer tools used by `dev` and `lint`.
	go install github.com/air-verse/air@$(AIR_VERSION)
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_VERSION)

# MODULE above is the current module; a MODULE given on the command line is the
# new one. NAME, BINARY and TITLE default from it; DRY=1 previews, FORCE=1 allows
# uncommitted changes. A real run then regenerates code and docs (the generated
# OpenAPI contract embeds the spec, which a text rename cannot reach).
rename: ## Rename the starter to your app: make rename MODULE=github.com/acme/shop-api [NAME=] [BINARY=] [TITLE=] [DRY=1]
ifeq ($(origin MODULE),command line)
	go run ./cmd/rename --module "$(MODULE)" --name "$(NAME)" --binary "$(BINARY)" --title "$(TITLE)" $(if $(DRY),--dry-run) $(if $(FORCE),--force)
ifndef DRY
	go mod tidy
	$(MAKE) generate docs-gen MODULE=$(MODULE)
endif
else
	@echo Usage: make rename MODULE=github.com/acme/shop-api [NAME=shop-api] [BINARY=shop] [TITLE="Shop API"] [DRY=1]
	@exit 1
endif

env: ## Warn when .env is missing.
	@$(if $(wildcard .env),echo Using existing .env file.,echo No .env found - copy .env.example to .env to customise local config.)

key-generate: ## Generate APP_KEY and write it to .env (KEY_FLAGS=--show prints it, --force replaces one).
	go run ./cmd/app key:generate $(KEY_FLAGS)

# ── Develop ──────────────────────────────────────────────────────────────────
build: ## Build the release binary with a baked-in version.
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/app

build-all: web-build build ## Build the developer SPA, then the Go binary.

web-build: ## Build the developer SPA into web/dev/dist (embedded by Go).
	npm --prefix web/dev ci
	npm --prefix web/dev run build

web-typecheck: ## Type-check the developer SPA.
	npm --prefix web/dev run typecheck

web-style-check: ## Check the SPA design tokens: WCAG AA contrast, type scale, no raw colours.
	npm --prefix web/dev run check:style

dev-web: ## Run the SPA dev server on :5173 with /dev/api proxied to :8080.
	npm --prefix web/dev run dev

dev: env ## Run the SPA dev server (:8080, HMR) and the API (:8081, air).
	go run ./cmd/app migrate
	node web/dev/scripts/dev.mjs

dev-api: env ## Run only the API with hot reload (air).
	go run ./cmd/app migrate
	air

run: ## Run the API once from source.
	go run ./cmd/app serve

serve: run ## Alias for `run`.

queue: ## Run the queue worker (River). Stop with Ctrl-C.
	go run ./cmd/app queue:work

schedule: ## Run the scheduler daemon. Stop with Ctrl-C.
	go run ./cmd/app schedule

# ── Quality ──────────────────────────────────────────────────────────────────
test: ## Run the unit test suite.
	go test $(TEST_FLAGS) ./...

test-race: ## Run the unit test suite with the race detector.
	go test -race $(TEST_FLAGS) ./...

test-cover: ## Run tests and write coverage.html.
	go test $(TEST_FLAGS) -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo Coverage report written to coverage.html

test-integration: ## Run the full suite against a real Postgres.
	go test -count=1 -race $(TEST_FLAGS) ./...

vet: ## Run go vet.
	go vet ./...

format: ## Format all Go code.
	gofmt -w .

fmt: format ## Alias for `format`.

format-check: ## Fail when any Go file is not gofmt-clean.
	@$(if $(shell gofmt -l .),$(error Unformatted files - run: make format :: $(shell gofmt -l .)),echo Go files are gofmt-clean.)

fmt-check: format-check ## Alias for `format-check`.

lint: ## Run golangci-lint.
	golangci-lint run ./...

lint-fix: ## Run golangci-lint and apply automatic fixes.
	golangci-lint run --fix ./...

tidy: ## Tidy go.mod and go.sum.
	go mod tidy

check: format-check vet lint test ## Format check + vet + lint + unit tests.

# ── Code generation ──────────────────────────────────────────────────────────
sqlc: ## Regenerate sqlc output in internal/dbgen.
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) generate

openapi: ## Regenerate OpenAPI-derived types in internal/contract.
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) -config api/oapi-codegen.yaml api/openapi.yaml

openapi-gen: ## Refresh the generated regions of api/openapi.yaml from module routes/DTOs.
	go run ./cmd/app gen openapi --all

openapi-check: ## Validate the spec + route drift and fail if the generated regions are stale.
	go run ./cmd/app openapi:check

generate: sqlc openapi openapi-gen ## Regenerate all generated code.

gen-check: skills-check ## Verify generated code, docs and skill copies are current; fails on drift.
	$(MAKE) sqlc openapi
	git diff --exit-code -- internal/dbgen internal/contract
	go run ./cmd/app gen openapi --all --check
	go run ./cmd/app gen docs --check

skills-sync: ## Copy the agent skills (.agents/skills, canonical) to .claude/skills for Claude Code.
	rm -rf .claude/skills
	mkdir -p .claude
	cp -r .agents/skills .claude/skills

skills-check: ## Fail when .claude/skills differs from .agents/skills (run `make skills-sync`).
	diff -r .agents/skills .claude/skills

# ── Documentation site (website/, content in docs/) ─────────────────────────
docs: ## Preview the documentation site with hot reload at http://localhost:4321.
	npm --prefix website ci
	npm --prefix website run dev

docs-build: ## Build the documentation site into website/dist and check every link.
	npm --prefix website ci
	npm --prefix website run build

docs-gen: ## Regenerate docs/reference (CLI, configuration, permissions, make) from the code.
	go run ./cmd/app gen docs

# ── Database ─────────────────────────────────────────────────────────────────
db-up: ## Start the local Postgres via docker compose.
	$(DOCKER_COMPOSE) up -d

db-down: ## Stop the local Postgres.
	$(DOCKER_COMPOSE) down

migrate: ## Apply pending migrations.
	go run ./cmd/app migrate

migrate-status: ## Show applied and pending migrations.
	go run ./cmd/app migrate:status

migrate-rollback: ## Roll back the most recent migration.
	go run ./cmd/app migrate:rollback

seed: ## Seed permissions, default tenant/branch/roles, super-admin and developer users.
	go run ./cmd/app db:seed

fresh: ## Roll back everything, migrate and seed. Destructive.
	go run ./cmd/app migrate:rollback 0
	$(MAKE) migrate seed

# ── Operations ───────────────────────────────────────────────────────────────
health: ## Run the application health checks.
	go run ./cmd/app health:check

config: ## Print the effective configuration with secrets masked.
	go run ./cmd/app config:show

version: ## Print the build version.
	go run ./cmd/app version

# ── Cleanup ──────────────────────────────────────────────────────────────────
clean: ## Remove build artifacts.
	go clean
	-$(RM) bin
	-$(RM) tmp
	-$(RM) coverage.out
	-$(RM) coverage.html

clean-all: clean ## Also remove local runtime state.
	-$(RM) var
