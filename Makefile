.PHONY: build build-all install clean lint mcp-config \
	test test-integration test-all test-doc test-fuzz test-coverage

BINARY_NAME := ctx
DIST_DIR := dist
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

# Platforms for cross-compilation
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

# Build for current platform
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) .

# Build for all platforms
build-all: $(PLATFORMS)

$(PLATFORMS):
	$(eval GOOS := $(word 1,$(subst /, ,$@)))
	$(eval GOARCH := $(word 2,$(subst /, ,$@)))
	$(eval EXT := $(if $(filter windows,$(GOOS)),.exe,))
	@mkdir -p $(DIST_DIR)/$(GOOS)-$(GOARCH)
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(GOOS)-$(GOARCH)/$(BINARY_NAME)$(EXT) .
	@echo "Built $(DIST_DIR)/$(GOOS)-$(GOARCH)/$(BINARY_NAME)$(EXT)"

# Build and install (binary, database, skill, hooks, CLAUDE.md)
install: build
	./$(BINARY_NAME) install
	@mkdir -p ~/.local/bin
	cp $(BINARY_NAME) ~/.local/bin/$(BINARY_NAME)
	@echo "Installed to ~/.local/bin/$(BINARY_NAME)"

# ---------------------------------------------------------------------------
# Tests
#
#   test              fast unit tests (default — no build tags)
#   test-integration  unit + integration tests that exec the built binary
#   test-all          alias for test-integration
#   test-doc          doc package round-trip subset
#   test-fuzz         query parser fuzz run
#   test-coverage     coverage profile + HTML report (includes integration)
# ---------------------------------------------------------------------------

test:
	go test ./...

test-integration:
	go test -tags=integration ./...

test-all: test-integration

test-doc:
	go test -run 'TestCorpus|TestCompose' ./internal/doc/...

test-fuzz:
	go test -fuzz=FuzzQueryParser -fuzztime=30s ./internal/query/

test-coverage:
	go test -tags=integration -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Show MCP configuration for Claude Desktop
mcp-config: build
	./$(BINARY_NAME) install --mcp

# Lint
lint:
	golangci-lint run ./...

# Clean
clean:
	rm -f $(BINARY_NAME) coverage.out coverage.html
	rm -rf $(DIST_DIR)
