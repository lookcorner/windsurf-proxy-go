# Windsurf Proxy Go - Makefile

.PHONY: build clean test docker run help wails-dev wails-build wails-build-all

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get

# Wails parameters
WAILS=wails
DESKTOP_DIR=desktop

# Binary names
BINARY_NAME=windsurf-proxy
BINARY_LINUX=$(BINARY_NAME)_linux_amd64
BINARY_DARWIN=$(BINARY_NAME)_darwin_arm64
BINARY_WINDOWS=$(BINARY_NAME)_windows_amd64.exe

# Build directory
BUILD_DIR=build

# Main package
MAIN_PACKAGE=./cmd/windsurf-proxy

help: ## Show this help
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build for current platform
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)"

build-all: ## Build for all platforms
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_LINUX) $(MAIN_PACKAGE)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_DARWIN) $(MAIN_PACKAGE)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_WINDOWS) $(MAIN_PACKAGE)
	@echo "Built all binaries in $(BUILD_DIR)/"

clean: ## Clean build artifacts
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)

test: ## Run tests
	$(GOTEST) -v ./...

deps: ## Download dependencies
	$(GOGET) ./...

docker: ## Build Docker image
	docker build -t windsurf-proxy:latest .

docker-run: ## Run Docker container
	docker run -p 8000:8000 -v $(PWD)/config.yaml:/app/config.yaml windsurf-proxy:latest

run: ## Run locally with default config
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PACKAGE)
	./$(BUILD_DIR)/$(BINARY_NAME) -c config.yaml.example

run-standalone: ## Run in standalone mode (requires Windsurf installed)
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PACKAGE)
	./$(BUILD_DIR)/$(BINARY_NAME) -c configs/standalone.yaml

lint: ## Run linter
	golangci-lint run ./...

fmt: ## Format code
	$(GOCMD) fmt ./...

check: ## Check for common issues
	$(GOCMD) vet ./...

install: ## Install binary to system
	$(GOBUILD) -o /usr/local/bin/$(BINARY_NAME) $(MAIN_PACKAGE)

uninstall: ## Uninstall binary
	rm -f /usr/local/bin/$(BINARY_NAME)

# Wails Desktop App
wails-dev: ## Run wails desktop app in development mode
	cd $(DESKTOP_DIR) && $(WAILS) dev

wails-build: ## Build wails desktop app for current platform
	cd $(DESKTOP_DIR) && $(WAILS) build

wails-build-all: ## Build wails desktop app for all platforms
	cd $(DESKTOP_DIR) && $(WAILS) build -platform darwin/amd64,darwin/arm64,windows/amd64,linux/amd64

wails-clean: ## Clean wails build artifacts
	rm -rf $(DESKTOP_DIR)/build/bin