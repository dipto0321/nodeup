# nodeup Makefile
#
# Wraps the most common development tasks so contributors don't have to
# remember the exact Go / golangci-lint / GoReleaser invocations.
#
# Usage:
#   make help          — list available targets
#   make build         — compile nodeup into ./bin/nodeup
#   make test          — run unit tests with race + coverage
#   make lint          — run golangci-lint with .golangci.yml
#   make fmt           — gofmt + goimports
#   make tidy          — go mod tidy
#   make run           — build and run nodeup (pass ARGS to forward)
#   make clean         — remove ./bin and coverage files
#   make release-snap  — build a snapshot release locally with GoReleaser
#   make release       — build AND publish via GoReleaser (CI does this)

# These vars let `make run ARGS="upgrade --dry-run"` work as expected.
ARGS ?=

# Avoid hardcoding a specific version in two places — read from go.mod.
BINARY := nodeup
BUILD_DIR := bin
COVERAGE_FILE := coverage.out
COVERAGE_HTML := coverage.html

# Go version pinning — matches .github/workflows/ci.yml.
GO_VERSION ?= 1.22

.PHONY: help
help: ## Show this help message
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: ## Compile nodeup into ./bin/nodeup
	@mkdir -p $(BUILD_DIR)
	go build -trimpath -ldflags "-s -w" -o $(BUILD_DIR)/$(BINARY) ./cmd/nodeup
	@echo "Built $(BUILD_DIR)/$(BINARY)"

.PHONY: test
test: ## Run unit tests with race + coverage
	go test -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
	@go tool cover -func=$(COVERAGE_FILE) | tail -1
	@go tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "Coverage report: $(COVERAGE_HTML)"

.PHONY: lint
lint: ## Run golangci-lint (must be installed: brew install golangci-lint)
	golangci-lint run ./...

.PHONY: fmt
fmt: ## Run gofmt and goimports on all Go files
	gofmt -s -w .
	@which goimports >/dev/null 2>&1 && goimports -local github.com/dipto0321/nodeup -w . || echo "goimports not installed, skipping (run: go install golang.org/x/tools/cmd/goimports@latest)"

.PHONY: tidy
tidy: ## Run go mod tidy
	go mod tidy

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: run
run: build ## Build and run nodeup (pass args via ARGS="...")
	./$(BUILD_DIR)/$(BINARY) $(ARGS)

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR) $(COVERAGE_FILE) $(COVERAGE_HTML)
	go clean -cache -testcache

.PHONY: install
install: ## Install nodeup into $GOPATH/bin
	go install -trimpath -ldflags "-s -w" ./cmd/nodeup

.PHONY: release-snap
release-snap: ## Build a snapshot release locally (no publish)
	goreleaser release --clean --snapshot --skip publish

.PHONY: release
release: ## Build AND publish a release (CI does this on tag push)
	goreleaser release --clean

.PHONY: ci
ci: tidy fmt vet lint test ## Run all CI checks locally (fmt, vet, lint, test)

.PHONY: deps
deps: ## Print key Go module versions
	@echo "go: $$(go version)"
	@grep -E '(cobra|huh|bubbletea|lipgloss|semver|gjson|yaml)' go.mod

.PHONY: verify
verify: ci build ## Full pre-commit verification (CI + build)