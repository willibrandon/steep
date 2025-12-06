.PHONY: build build-agent build-repl build-repl-daemon build-repl-ext build-all test test-short test-integration test-agent test-repl test-repl-integration test-coverage bench clean run run-dev run-agent run-agent-dev install-agent uninstall-agent start-agent stop-agent status-agent run-repl install-repl uninstall-repl start-repl stop-repl status-repl help

# Force cmd.exe on Windows to avoid shell inconsistencies
ifeq ($(OS),Windows_NT)
    SHELL := cmd.exe
endif

# Binary names
BINARY_NAME=steep
AGENT_BINARY_NAME=steep-agent
REPL_BINARY_NAME=steep-repl

# Rust/pgrx extension
REPL_EXT_DIR=extensions/steep_repl

# Build directory
BUILD_DIR=bin

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOCLEAN=$(GOCMD) clean
GOGET=$(GOCMD) get

# Steep configuration file path
CONFIG=~/.config/steep/config.yaml

# Optional runtime arguments (e.g., make run-dev ARGS="--debug")
ARGS ?=

# Detect .exe suffix on Windows
ifeq ($(OS),Windows_NT)
    BINARY_EXT=.exe
else
    BINARY_EXT=
endif

help: ## Display this help screen
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build the TUI application
	@echo "Building $(BINARY_NAME)..."
ifeq ($(OS),Windows_NT)
	@if not exist $(BUILD_DIR) mkdir $(BUILD_DIR)
else
	@mkdir -p $(BUILD_DIR)
endif
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)$(BINARY_EXT) cmd/steep/main.go
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)$(BINARY_EXT)"

build-agent: ## Build the agent daemon
	@echo "Building $(AGENT_BINARY_NAME)..."
ifeq ($(OS),Windows_NT)
	@if not exist $(BUILD_DIR) mkdir $(BUILD_DIR)
else
	@mkdir -p $(BUILD_DIR)
endif
	$(GOBUILD) -o $(BUILD_DIR)/$(AGENT_BINARY_NAME)$(BINARY_EXT) cmd/steep-agent/main.go
	@echo "Build complete: $(BUILD_DIR)/$(AGENT_BINARY_NAME)$(BINARY_EXT)"

build-repl-daemon: ## Build the steep-repl replication daemon
	@echo "Building $(REPL_BINARY_NAME) daemon..."
ifeq ($(OS),Windows_NT)
	@if not exist $(BUILD_DIR) mkdir $(BUILD_DIR)
else
	@mkdir -p $(BUILD_DIR)
endif
	$(GOBUILD) -o $(BUILD_DIR)/$(REPL_BINARY_NAME)$(BINARY_EXT) ./cmd/steep-repl
	@echo "Build complete: $(BUILD_DIR)/$(REPL_BINARY_NAME)$(BINARY_EXT)"

build-repl-ext: ## Build the steep_repl PostgreSQL extension
	@echo "Building steep_repl extension..."
ifeq ($(OS),Windows_NT)
	cd $(REPL_EXT_DIR) && cargo build --features pg18
else
	cd $(REPL_EXT_DIR) && SDKROOT=$$(xcrun --show-sdk-path 2>/dev/null || echo "") cargo build --features pg18
endif
	@echo "Extension built: $(REPL_EXT_DIR)/target/debug/libsteep_repl.dylib"

build-repl: build-repl-daemon build-repl-ext ## Build both replication daemon and extension

build-all: build build-agent build-repl-daemon ## Build TUI, agent, and repl daemon
ifndef SKIP_REPL_EXT
build-all: build-repl-ext
endif

test: ## Run all tests
	@echo "Running tests..."
	$(GOTEST) -v -count=1 ./...

test-short: ## Run tests (skip integration)
	@echo "Running short tests..."
	$(GOTEST) -short -count=1 -v ./...

test-integration: ## Run integration tests only
	@echo "Running integration tests..."
	$(GOTEST) -v -count=1 ./tests/integration/...

test-all: test test-repl ## Run all tests including repl extension tests
	@echo "All tests completed."

bench: ## Run performance benchmarks
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./tests/integration/queries/ -run=^$$

test-coverage: ## Run tests with coverage
	@echo "Running tests with coverage..."
ifeq ($(OS),Windows_NT)
	@if not exist coverage mkdir coverage
else
	@mkdir -p coverage
endif
	$(GOTEST) -v -coverprofile=coverage/coverage.out ./...
	$(GOCMD) tool cover -html=coverage/coverage.out -o coverage/coverage.html
	@echo "Coverage report generated: coverage/coverage.html"

clean: ## Clean build artifacts
	@echo "Cleaning..."
	$(GOCLEAN)
ifeq ($(OS),Windows_NT)
	@if exist $(BUILD_DIR) rmdir /s /q $(BUILD_DIR)
	@if exist coverage rmdir /s /q coverage
else
	@rm -rf $(BUILD_DIR)
	@rm -rf coverage
endif
	@echo "Clean complete"

clean-repl-ext: ## Clean steep_repl extension build artifacts
	@echo "Cleaning steep_repl extension..."
	cd $(REPL_EXT_DIR) && cargo clean
	@echo "Extension clean complete"

clean-all: clean clean-repl-ext ## Clean all build artifacts including extension

run: build ## Build and run the application
	@echo "Running $(BINARY_NAME)..."
	@$(BUILD_DIR)/$(BINARY_NAME)$(BINARY_EXT) --banner $(ARGS)

run-dev: build ## Run with config.yaml
	@echo "Running $(BINARY_NAME) with config" $(CONFIG) "..."
	@$(BUILD_DIR)/$(BINARY_NAME)$(BINARY_EXT) --config $(CONFIG) $(ARGS)

run-agent: build-agent ## Run agent in foreground
	@echo "Running $(AGENT_BINARY_NAME) in foreground..."
	@$(BUILD_DIR)/$(AGENT_BINARY_NAME)$(BINARY_EXT) run $(ARGS)

run-agent-dev: build-agent ## Run agent with config.yaml
	@echo "Running $(AGENT_BINARY_NAME) with config" $(CONFIG) "..."
	@$(BUILD_DIR)/$(AGENT_BINARY_NAME)$(BINARY_EXT) run --config $(CONFIG) $(ARGS)

test-agent: build-agent ## Run agent-specific tests
	@echo "Running agent tests..."
	$(GOTEST) -v -count=1 ./internal/agent/...

test-repl: ## Run steep_repl extension tests (requires PG18)
	@echo "Running steep_repl extension tests..."
ifeq ($(OS),Windows_NT)
	cd $(REPL_EXT_DIR) && cargo pgrx test pg18
else
	cd $(REPL_EXT_DIR) && SDKROOT=$$(xcrun --show-sdk-path 2>/dev/null || echo "") cargo pgrx test pg18
endif

test-repl-integration: ## Run steep-repl integration tests (requires Docker)
	@echo "Running steep-repl integration tests..."
	$(GOTEST) -v -count=1 ./tests/integration/repl/...

install-agent: build-agent ## Install agent as a system service (user mode)
	@echo "Installing $(AGENT_BINARY_NAME) as user service..."
	@$(BUILD_DIR)/$(AGENT_BINARY_NAME)$(BINARY_EXT) install --user
	@echo "Service installed. Start with: make start-agent"

uninstall-agent: ## Uninstall agent service
	@echo "Uninstalling $(AGENT_BINARY_NAME) service..."
	@$(BUILD_DIR)/$(AGENT_BINARY_NAME)$(BINARY_EXT) uninstall
	@echo "Service uninstalled"

start-agent: ## Start the installed agent service
	@echo "Starting $(AGENT_BINARY_NAME) service..."
	@$(BUILD_DIR)/$(AGENT_BINARY_NAME)$(BINARY_EXT) start

stop-agent: ## Stop the running agent service
	@echo "Stopping $(AGENT_BINARY_NAME) service..."
	@$(BUILD_DIR)/$(AGENT_BINARY_NAME)$(BINARY_EXT) stop

status-agent: ## Show agent service status
	@$(BUILD_DIR)/$(AGENT_BINARY_NAME)$(BINARY_EXT) status

run-repl: build-repl-daemon ## Run repl daemon in foreground
	@echo "Running $(REPL_BINARY_NAME) in foreground..."
	@$(BUILD_DIR)/$(REPL_BINARY_NAME)$(BINARY_EXT) run $(ARGS)

install-repl: build-repl-daemon ## Install repl daemon as a system service (user mode)
	@echo "Installing $(REPL_BINARY_NAME) as user service..."
	@$(BUILD_DIR)/$(REPL_BINARY_NAME)$(BINARY_EXT) install --user
	@echo "Service installed. Start with: make start-repl"

uninstall-repl: ## Uninstall repl daemon service
	@echo "Uninstalling $(REPL_BINARY_NAME) service..."
	@$(BUILD_DIR)/$(REPL_BINARY_NAME)$(BINARY_EXT) uninstall
	@echo "Service uninstalled"

start-repl: ## Start the installed repl daemon service
	@echo "Starting $(REPL_BINARY_NAME) service..."
	@$(BUILD_DIR)/$(REPL_BINARY_NAME)$(BINARY_EXT) start

stop-repl: ## Stop the running repl daemon service
	@echo "Stopping $(REPL_BINARY_NAME) service..."
	@$(BUILD_DIR)/$(REPL_BINARY_NAME)$(BINARY_EXT) stop

status-repl: ## Show repl daemon service status
	@$(BUILD_DIR)/$(REPL_BINARY_NAME)$(BINARY_EXT) status

.DEFAULT_GOAL := help
