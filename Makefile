# ktpl — developer entry points. Run `make help` for the list of targets.

BINARY        := ktpl
PKG           := github.com/bricks-it/ktpl
VERSION       ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS       := -s -w -X $(PKG)/internal/cli.version=$(VERSION)
GOLANGCI_VER  := v2.14.0
GOLANGCI_LINT ?= $(shell command -v golangci-lint 2>/dev/null || echo go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VER))
GO_FILES       = $(shell find . -name '*.go' -not -path './dist/*')

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the static binary into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY)$(if $(filter windows,$(GOOS)),.exe,) ./cmd/ktpl

.PHONY: install
install: ## Install ktpl into $GOBIN
	CGO_ENABLED=0 go install -trimpath -ldflags '$(LDFLAGS)' ./cmd/ktpl

.PHONY: test
test: ## Run unit and golden tests
	go test ./...

.PHONY: test-race
test-race: ## Run tests with the race detector
	go test -race ./...

.PHONY: cover
cover: ## Run tests with coverage (coverage.out)
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n 1

.PHONY: golden
golden: ## Regenerate examples/*/rendered (REVIEW the diff afterwards)
	go test ./internal/cli -run TestGolden -update
	@git status --short examples/ || true

.PHONY: fuzz
fuzz: ## Fuzz the path and identity parsers (FUZZTIME=30s)
	go test ./internal/path -run '^$$' -fuzz FuzzParse -fuzztime $(or $(FUZZTIME),30s)
	go test ./internal/object -run '^$$' -fuzz FuzzParseRef -fuzztime $(or $(FUZZTIME),30s)

.PHONY: lint
lint: ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

.PHONY: fmt
fmt: ## Format Go code
	gofmt -w $(GO_FILES)

.PHONY: fmt-check
fmt-check: ## Fail if Go code is not formatted
	@out="$$(gofmt -l $(GO_FILES))"; if [ -n "$$out" ]; then echo "unformatted files:"; echo "$$out"; exit 1; fi

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: tidy
tidy: ## Run go mod tidy
	go mod tidy

.PHONY: tidy-check
tidy-check: ## Fail if go.mod/go.sum are not tidy
	go mod tidy -diff

.PHONY: preflight
preflight: fmt-check tidy-check vet lint test-race build ## All checks required before declaring a task done
	@echo "preflight OK"

.PHONY: snapshot
snapshot: ## Build a local GoReleaser snapshot into dist/
	goreleaser release --snapshot --clean

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin dist coverage.out
