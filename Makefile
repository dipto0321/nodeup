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

# Go version pinning — matches the `go` directive in go.mod and
# .github/workflows/ci.yml. Bump all three together when upgrading.
GO_VERSION ?= 1.24

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

# Drive a single GitHub issue from branch → PR → merge.
# Usage: make next-issue ISSUE=16
# For human contributors, this is a thin wrapper that prints the
# standing-orders reminder. The actual orchestration is in the
# `.claude/skills/issue-workflow/SKILL.md` skill for AI sessions,
# or driven manually by following CONTRIBUTING.md for humans.
.PHONY: next-issue
next-issue: ## Print the standing-orders reminder for ISSUE; humans drive the workflow from CONTRIBUTING.md
	@if [ -z "$(ISSUE)" ]; then \
		echo "usage: make next-issue ISSUE=<issue#>"; \
		exit 1; \
	fi
	@echo "Issue #$(ISSUE) — drive the workflow per CONTRIBUTING.md:"
	@echo "  1. Sync main and create the branch:"
	@echo "       git fetch origin main && git checkout main && git pull --ff-only origin main"
	@echo "       git checkout -b <type>/<scope>/<slug> origin/main"
	@echo "  2. Implement + test + lint, commit with a Conventional Commits subject"
	@echo "       make ci"
	@echo "  3. Push and open the PR:"
	@echo "       git push -u origin <branch>"
	@echo "       gh pr create --base main --head <branch> --title '<subject>' --body-file /tmp/pr-body-$(ISSUE).md"
	@echo "  4. Watch CI and merge:"
	@echo "       gh pr checks <PR#> --watch --fail-fast"
	@echo "       make finish-pr ISSUE=$(ISSUE) PR=<PR#>"
	@echo ""
	@echo "AI sessions should invoke .claude/skills/issue-workflow/SKILL.md instead."

# Squash-merge the PR linked to ISSUE. Use after the branch is pushed
# and CI is green. Uses --admin because branch protection requires 1
# approving review but enforce_admins is disabled for solo author work.
# Usage: make finish-pr ISSUE=16 PR=23   (PR auto-detected if omitted)
.PHONY: finish-pr
finish-pr: ## Squash-merge the PR for ISSUE and verify the issue auto-closes
	@if [ -z "$(ISSUE)" ]; then \
		echo "usage: make finish-pr ISSUE=<issue#> [PR=<pr#>]"; \
		exit 1; \
	fi
	@PR="$${PR:-$(shell gh pr list --state open --json number,headRefName --jq '.[] | select(.headRefName|test("$(ISSUE)")) | .number' | head -1)}"; \
	if [ -z "$$PR" ]; then \
		echo "no open PR found for issue $(ISSUE). Run \`make next-issue ISSUE=$(ISSUE)\` first."; \
		exit 1; \
	fi; \
	echo "==> squash-merging PR #$$PR with admin override (closes #$(ISSUE))"; \
	gh pr merge "$$PR" --squash --delete-branch --admin \
	    --body "Closes #$(ISSUE). Squash-merged per CONTRIBUTING.md."; \
	echo "==> syncing local main"; \
	git checkout main && git pull --ff-only origin main; \
	git remote prune origin; \
	echo "==> verifying issue #$(ISSUE) auto-closed"; \
	STATE=$$(gh issue view $(ISSUE) --json state --jq .state); \
	if [ "$$STATE" = "CLOSED" ]; then \
		echo "✓ issue #$(ISSUE) is CLOSED"; \
	else \
		echo "warning: issue #$(ISSUE) is still $$STATE — closing manually"; \
		gh issue close $(ISSUE) -c "Closed by PR #$$PR. Squash-merged per CONTRIBUTING.md."; \
	fi