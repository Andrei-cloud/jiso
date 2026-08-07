# define the shell to bash
SHELL := /bin/bash

# help target for showing usage
.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

run: ## Run the service
	@go run ./cmd/main.go

build: ## Build the service
	@CGO_ENABLED=0 go build -o bin/jiso ./cmd/

build-linux: ## Build the service for linux
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/jiso ./cmd/

# default target, when make executed without arguments
all: help

