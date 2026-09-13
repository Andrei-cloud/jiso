# define the shell to bash
SHELL := /bin/bash

# build-time stamping for -v/--version
VERSION_PKG := jiso/internal/version
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILT_AT := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X $(VERSION_PKG).Version=$(VERSION) -X $(VERSION_PKG).Commit=$(COMMIT) -X $(VERSION_PKG).BuiltAt=$(BUILT_AT)

# help target for showing usage
.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

run: ## Run the service
	@go run ./cmd/main.go

build: ## Build the service
	@CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/jiso ./cmd/

build-linux: ## Build the service for linux
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/jiso ./cmd/

# default target, when make executed without arguments
all: help

# --- quality gates -------------------------------------------------------
# Local only (no CI workflow by decision). Lint/formatter rules live in
# .golangci.yml; file size is ratcheted by internal/repohealth, not a linter.
# `make qa` is the gate: it is RED until the adopted-gate backlog is cleared.

# `vet` runs golangci's govet rather than raw `go vet` on purpose: the six
# moov-io network.Header implementations in internal/utils must declare
# WriteTo/ReadFrom as (int, error) to satisfy that upstream interface, which is
# what stdmethods complains about. Raw `go vet` has no inline suppression, so
# those six would fail forever; golangci's govet honours the local //nolint at
# them. stdmethods is NOT disabled -- a scratch method violating it is still
# reported -- and nolintlint (allow-unused: false) drops the annotations the
# moment they stop being needed.
GOLANGCI_LINT ?= golangci-lint
DEADCODE ?= deadcode
COVER_PROFILE ?= coverage.out

.PHONY: fmt
fmt: ## Format Go sources (gofumpt + goimports, per .golangci.yml)
	@$(GOLANGCI_LINT) fmt ./...

.PHONY: fmt-check
fmt-check: ## Fail when Go sources are not gofumpt/goimports clean
	@diff=$$($(GOLANGCI_LINT) fmt --diff ./... 2>&1); \
	if [ -n "$$diff" ]; then \
		echo "source is not formatted -- run 'make fmt':"; \
		printf '%s\n' "$$diff" | sed -n 's/^--- \([^ ]*\)\.orig.*/  \1/p' | sort -u | head -20; \
		exit 1; \
	fi
	@echo "fmt: clean"

.PHONY: vet
vet: ## go vet's analyzers, via golangci's govet
	@$(GOLANGCI_LINT) run --enable-only=govet ./...
	@echo "vet: clean"

.PHONY: lint
lint: ## golangci-lint (the adopted gate; see .golangci.yml)
	@$(GOLANGCI_LINT) run ./...

.PHONY: deadcode
deadcode: ## Fail on declarations unreachable even from tests
	@command -v $(DEADCODE) >/dev/null 2>&1 || { echo "missing: go install golang.org/x/tools/cmd/deadcode@latest"; exit 1; }
	@out=$$($(DEADCODE) -test ./... 2>&1); \
	if printf '%s\n' "$$out" | grep -q 'packages contain errors'; then \
		echo "deadcode could not analyse the packages:"; printf '%s\n' "$$out" | head -5; exit 1; \
	fi; \
	dead=$$(printf '%s\n' "$$out" | grep 'unreachable func' | sort); \
	if [ -n "$$dead" ]; then echo "unreachable even from tests:"; printf '%s\n' "$$dead"; exit 1; fi; \
	echo "deadcode: clean (prod-reachability: $$($(DEADCODE) -test=false ./cmd/ 2>/dev/null | grep -c 'unreachable func') functions reachable only from tests)"

.PHONY: test
test: ## go test ./...
	@go test ./...

.PHONY: race
race: ## go test -race ./...
	@go test -race ./...

.PHONY: cover
cover: ## Module coverage from a merged -coverpkg profile
	@go test -coverpkg=./... -coverprofile=$(COVER_PROFILE) ./... > /dev/null 2>&1 || true
	@go tool cover -func=$(COVER_PROFILE) | tail -1

.PHONY: goldens
goldens: ## Regenerate TUI golden frames (review the diff before committing)
	@go test $$(go list ./internal/tui/... | grep -v /bridge) -update

# The full local gate. The three hygiene guards (7-bit ASCII goldens, file
# line budget, golden width budget) run as part of ./internal/tui and
# ./internal/repohealth, so they are covered by `test` and `race`.
.PHONY: qa
qa: fmt-check vet lint deadcode test race ## The full local gate
	@echo "qa: all gates green"
