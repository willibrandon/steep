.PHONY: build test test-short test-integration test-coverage bench clean run run-dev help

# Force cmd.exe on Windows to avoid shell inconsistencies
ifeq ($(OS),Windows_NT)
    SHELL := cmd.exe
endif

# Binary name
BINARY_NAME=steep

# Build directory
BUILD_DIR=bin

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOCLEAN=$(GOCMD) clean
GOGET=$(GOCMD) get

# Detect .exe suffix on Windows
ifeq ($(OS),Windows_NT)
    BINARY_EXT=.exe
else
    BINARY_EXT=
endif

help: ## Display this help screen
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build the application
	@echo "Building $(BINARY_NAME)..."
ifeq ($(OS),Windows_NT)
	@if not exist $(BUILD_DIR) mkdir $(BUILD_DIR)
else
	@mkdir -p $(BUILD_DIR)
endif
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)$(BINARY_EXT) cmd/steep/main.go
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)$(BINARY_EXT)"

test: ## Run all tests
	@echo "Running tests..."
	$(GOTEST) -v -count=1 ./...

test-short: ## Run tests (skip integration)
	@echo "Running short tests..."
	$(GOTEST) -short -count=1 -v ./...

test-integration: ## Run integration tests only
	@echo "Running integration tests..."
	$(GOTEST) -v -count=1 ./tests/integration/...

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

run: build ## Build and run the application
	@echo "Running $(BINARY_NAME)..."
	@$(BUILD_DIR)/$(BINARY_NAME)$(BINARY_EXT) --banner --debug

run-dev: build ## Run with local config.yaml and debug (for Docker replication testing)
	@echo "Running $(BINARY_NAME) with local config and debug..."
	@PGPASSWORD=postgres $(BUILD_DIR)/$(BINARY_NAME)$(BINARY_EXT) --config ./config.yaml --debug

.DEFAULT_GOAL := help
