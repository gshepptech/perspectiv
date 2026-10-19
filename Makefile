# perspectiv: the single orchestrator for build, test, lint, codegen and the
# local dev loop. Run `make help` for the target list.

SHELL := bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := help
MAKEFLAGS += --no-print-directory

MODULE   := perspectiv
BINARIES := perspectiv placectl

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

BUILDINFO := $(MODULE)/internal/api/buildinfo
LDFLAGS   := -s -w -X $(BUILDINFO).Version=$(VERSION) -X $(BUILDINFO).Commit=$(COMMIT) -X $(BUILDINFO).Date=$(DATE)

# Statement-coverage floors that make check enforces, as <import path>=<percent>.
# scripts/coverfloor documents how coverage is measured and merged.
COVERAGE_FLOORS := \
	$(MODULE)/internal/auth=80 \
	$(MODULE)/internal/lore=80 \
	$(MODULE)/internal/vote=80 \
	$(MODULE)/internal/moderation=80

TMP_DIR       := $(CURDIR)/tmp
COVER_DIR     := $(TMP_DIR)/coverage
REPORT_DIR    := $(TMP_DIR)/test-results
COVER_PROFILES ?= $(COVER_DIR)/unit.out $(COVER_DIR)/integration.out

WEB_DIR := web
HAS_WEB := $(wildcard $(WEB_DIR)/package.json)
PNPM    := pnpm --dir $(WEB_DIR)

# web/embed.go embeds web/dist, so every target that compiles the Go module
# (lint, vet, test, test-integration, the binaries, migrate, the dev loop)
# depends on the bundle. It is rebuilt only when a web source is newer than it.
WEB_DIST    := $(WEB_DIR)/dist/index.html
WEB_SOURCES := $(if $(HAS_WEB),$(shell find $(WEB_DIR)/src $(WEB_DIR)/public -type f 2>/dev/null) \
	$(wildcard $(WEB_DIR)/index.html $(WEB_DIR)/package.json $(WEB_DIR)/pnpm-lock.yaml \
	$(WEB_DIR)/pnpm-workspace.yaml $(WEB_DIR)/vite.config.ts $(WEB_DIR)/tsconfig*.json))

# ---------------------------------------------------------------------------
# Pinned Go tools. Each installs into bin/tools/<name>-<version>/ with the Go
# toolchain go.mod selects, so a version bump installs alongside the old one
# and nothing on the developer's global PATH is replaced.

TOOLS_BIN := $(CURDIR)/bin/tools

GOLANGCI_LINT_VERSION := v2.14.0
GOTESTSUM_VERSION     := v1.13.0
AIR_VERSION           := v1.67.4
GOOSE_VERSION         := v3.29.0
SQLC_VERSION          := v1.31.1
BUF_VERSION           := v1.74.0
# Code generators track the runtime modules go.mod already pins.
PROTOBUF_VERSION = $(shell go list -m -f '{{.Version}}' google.golang.org/protobuf)
CONNECT_VERSION  = $(shell go list -m -f '{{.Version}}' connectrpc.com/connect)

# $(call tool,VAR,package,version) defines VAR as the binary path and a rule to install it.
define tool
$(1) := $(TOOLS_BIN)/$(notdir $(2))-$(3)/$(notdir $(2))
$$($(1)):
	@echo "installing $(notdir $(2)) $(3)"
	GOBIN=$$(dir $$@) go install $(2)@$(3)
endef

$(eval $(call tool,GOLANGCI_LINT,github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION)))
$(eval $(call tool,GOTESTSUM,gotest.tools/gotestsum,$(GOTESTSUM_VERSION)))
$(eval $(call tool,AIR,github.com/air-verse/air,$(AIR_VERSION)))
$(eval $(call tool,GOOSE,github.com/pressly/goose/v3/cmd/goose,$(GOOSE_VERSION)))
$(eval $(call tool,SQLC,github.com/sqlc-dev/sqlc/cmd/sqlc,$(SQLC_VERSION)))
$(eval $(call tool,BUF,github.com/bufbuild/buf/cmd/buf,$(BUF_VERSION)))

PROTOC_GEN_GO         = $(TOOLS_BIN)/protoc-gen-go-$(PROTOBUF_VERSION)/protoc-gen-go
PROTOC_GEN_CONNECT_GO = $(TOOLS_BIN)/protoc-gen-connect-go-$(CONNECT_VERSION)/protoc-gen-connect-go

GO_TOOLS = $(GOLANGCI_LINT) $(GOTESTSUM) $(AIR) $(GOOSE) $(SQLC) $(BUF) $(PROTOC_GEN_GO) $(PROTOC_GEN_CONNECT_GO)

DEV := AIR="$(AIR)" GOOSE="$(GOOSE)" scripts/dev.sh

BIN_TARGETS := $(addprefix bin/,$(BINARIES))

.PHONY: help tools dev dev-up dev-down dev-status dev-logs dev-migrate dev-migrate-down \
	dev-grafana dev-grafana-down build web-deps web-build $(BIN_TARGETS) test test-integration \
	coverage-floors e2e lint vet fmt fmt-fix generate migrate new-migration check updates clean

help: ## List targets
	@grep -hE '^[a-zA-Z0-9_./-]+:.*## ' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  %-20s %s\n", $$1, $$2}'

tools: $(GO_TOOLS) ## Install pinned Go tools into bin/tools (once per machine)
	@command -v shellcheck >/dev/null || echo "shellcheck is not on PATH: brew install shellcheck (macOS) or apt-get install shellcheck"
	@command -v pnpm >/dev/null || echo "pnpm is not on PATH: corepack enable pnpm"

$(PROTOC_GEN_GO):
	GOBIN=$(dir $@) go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOBUF_VERSION)

$(PROTOC_GEN_CONNECT_GO):
	GOBIN=$(dir $@) go install connectrpc.com/connect/cmd/protoc-gen-connect-go@$(CONNECT_VERSION)

# ---------------------------------------------------------------------------
# Dev loop: Postgres 18 and Mailpit containers, the server under Air, Vite.

dev: dev-up ## Alias for dev-up

dev-up: $(AIR) $(GOOSE) $(WEB_DIST) ## Start Postgres, Mailpit, migrations, server (Air) and web (Vite) in the background
	@$(DEV) up

dev-down: ## Stop the dev loop; the Postgres volume is kept
	@$(DEV) down

dev-status: ## Show which dev services run and whether /healthz answers
	@$(DEV) status

dev-logs: ## Tail server and web logs
	@$(DEV) logs

dev-migrate: $(GOOSE) ## Apply pending migrations to the dev database
	@$(DEV) migrate

dev-migrate-down: $(GOOSE) ## Roll back the last goose migration on the dev database
	@$(DEV) migrate-down

dev-grafana: ## Start Grafana, Prometheus, Tempo and Loki for the dev loop
	@$(DEV) grafana-up

dev-grafana-down: ## Stop the observability stack
	@$(DEV) grafana-down

# ---------------------------------------------------------------------------
# Build

build: web-build $(BIN_TARGETS) ## Build the web bundle and every binary into bin/

web-deps:
ifneq ($(HAS_WEB),)
	@if [[ -f $(WEB_DIR)/pnpm-lock.yaml ]]; then $(PNPM) install --frozen-lockfile; else $(PNPM) install; fi
endif

web-build: web-deps ## Build the SPA into web/dist (embedded by the server)
ifneq ($(HAS_WEB),)
	$(PNPM) run build
endif

$(WEB_DIST): $(WEB_SOURCES)
	@$(MAKE) web-build
	@touch $@

bin/perspectiv: $(WEB_DIST)

$(BIN_TARGETS): bin/%:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $@ ./cmd/$*

# ---------------------------------------------------------------------------
# Tests and gates

test: $(GOTESTSUM) $(WEB_DIST) ## Unit tier: Go (race, no Docker) and web unit tests
	@mkdir -p $(COVER_DIR) $(REPORT_DIR)
	$(GOTESTSUM) --format pkgname --junitfile $(REPORT_DIR)/unit.xml -- \
		-race -count=1 -coverprofile=$(COVER_DIR)/unit.out ./...
ifneq ($(HAS_WEB),)
	@$(MAKE) web-deps
	CI=1 $(PNPM) run --if-present test
endif

test-integration: $(GOTESTSUM) $(WEB_DIST) ## Integration tier: real postgres:18-alpine through testcontainers
	@mkdir -p $(COVER_DIR) $(REPORT_DIR)
	$(GOTESTSUM) --format pkgname --junitfile $(REPORT_DIR)/integration.xml -- \
		-race -count=1 -tags integration -coverprofile=$(COVER_DIR)/integration.out ./...

coverage-floors: ## Enforce per-package coverage floors over the unit and integration profiles
	go run ./scripts/coverfloor $(addprefix -floor ,$(COVERAGE_FLOORS)) $(COVER_PROFILES)

e2e: ## Playwright E2E against the running dev loop (make dev-up first)
	@$(DEV) e2e

lint: $(GOLANGCI_LINT) $(WEB_DIST) ## golangci-lint, module layout, design-pass order, shellcheck, web lint
	$(GOLANGCI_LINT) run ./...
	go run ./scripts/layoutcheck
	go run ./scripts/designcheck
	@command -v shellcheck >/dev/null || { echo "shellcheck is not on PATH: brew install shellcheck"; exit 1; }
	shellcheck scripts/*.sh
ifneq ($(HAS_WEB),)
	@$(MAKE) web-deps
	$(PNPM) run --if-present lint
	$(PNPM) run --if-present typecheck
endif

vet: $(WEB_DIST) ## go vet over both test tiers
	go vet ./...
	go vet -tags integration ./...

fmt: $(GOLANGCI_LINT) ## Verify gofmt and goimports formatting; fails on any diff
	$(GOLANGCI_LINT) fmt --diff ./...

fmt-fix: $(GOLANGCI_LINT) ## Rewrite files with gofmt and goimports
	$(GOLANGCI_LINT) fmt ./...

check: ## The full local gate: lint vet fmt test test-integration and coverage floors
	@$(MAKE) lint
	@$(MAKE) vet
	@$(MAKE) fmt
	@$(MAKE) test
	@$(MAKE) test-integration
	@$(MAKE) coverage-floors

# ---------------------------------------------------------------------------
# Code generation and migrations

generate: $(SQLC) $(BUF) $(PROTOC_GEN_GO) $(PROTOC_GEN_CONNECT_GO) ## Regenerate sqlc accessors and buf (Go and TS) code
	@for cfg in $$(find internal -name sqlc.yaml -not -path '*/node_modules/*' | sort); do \
		echo "sqlc generate -f $$cfg"; $(SQLC) generate -f "$$cfg"; \
	done
	@if [[ -f buf.gen.yaml ]]; then \
		PATH="$(dir $(PROTOC_GEN_GO)):$(dir $(PROTOC_GEN_CONNECT_GO)):$(CURDIR)/$(WEB_DIR)/node_modules/.bin:$$PATH" $(BUF) generate; \
	fi

migrate: $(WEB_DIST) ## Apply migrations to DATABASE_URL with the production binary path
	go run ./cmd/perspectiv migrate

new-migration: ## Create the next goose migration: make new-migration NAME=snake_case
	@scripts/new-migration.sh "$(NAME)"

updates: ## List available Go module (and web dependency) updates
	go list -u -m all
ifneq ($(HAS_WEB),)
	$(PNPM) outdated || true
endif

clean: ## Remove build output, coverage and test reports (keeps bin/tools)
	rm -rf $(TMP_DIR)/coverage $(TMP_DIR)/test-results $(BIN_TARGETS)
