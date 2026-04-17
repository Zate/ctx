.PHONY: test test-unit test-fuzz test-doc build install clean mcp-config build-all lint

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
	./$(BINARY_NAME) install --bin-dir ~/.local/bin

# All tests (includes corpus round-trip via test-doc dependency)
test: test-doc
	go test -v ./...

# Corpus round-trip tests for doc package
test-doc:
	go test -v -run 'TestCorpus|TestCompose' ./internal/doc/...

# Unit tests only
test-unit:
	go test -v -short ./internal/...

# Fuzz testing
test-fuzz:
	go test -fuzz=FuzzQueryParser -fuzztime=30s ./internal/query/

# Coverage
test-coverage:
	go test -coverprofile=coverage.out ./...
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
