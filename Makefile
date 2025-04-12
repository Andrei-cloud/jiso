# define the shell to bash
SHELL := /bin/bash

# help target for showing usage
.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

run: ## Run the service
	@go run ./cmd/main.go -host localhost -port 9999 -file ./transactions/transaction.json -spec-file ./specs/spec_bcp.json

build: ## Build the service
	@go build -o jiso ./cmd/main.go

# default target, when make executed without arguments
all: help

