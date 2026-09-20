.PHONY: help tidy fmt lint vet test build run clean all

BINARY  ?= traffic-gen
CMD     ?= ./cmd/traffic-gen
BIN_DIR ?= bin

GOFLAGS     := -trimpath
LDFLAGS     := -s -w

VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS_FULL := $(LDFLAGS) -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.DEFAULT_GOAL := help

help: ## Show available commands
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

tidy: ## Tidy go.mod/go.sum
	go mod tidy

fmt: ## Format source code
	@gofmt -s -w $$(find . -name '*.go' -not -path './vendor/*')

lint: ## Run linter
	@command -v golangci-lint >/dev/null 2>&1 || { echo "install golangci-lint first"; exit 1; }
	golangci-lint run ./...

vet: ## Run go vet
	go vet ./...

test: ## Run tests
	go test -race -count=1 -coverprofile=coverage.out ./...

build: ## Build binary
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS_FULL)" -o $(BIN_DIR)/$(BINARY) $(CMD)

run: build ## Build and run
	./$(BIN_DIR)/$(BINARY)

all: ## Full pipeline
	$(MAKE) tidy
	$(MAKE) fmt
	$(MAKE) lint
	$(MAKE) test
	$(MAKE) build

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)/

clean-cache: ## Clean global Go caches
	go clean -cache -testcache