# Genie CLI Makefile

# Build variables
BINARY_NAME=genie
GIT_COMMIT = $(shell git rev-parse --short HEAD)
GIT_DIRTY  = $(shell test -n "`git status --porcelain`" && echo "-dirty" || echo "")
GIT_VERSION=${GIT_COMMIT}${GIT_DIRTY}

# Optional: inject Google OAuth client for Calendar + Contacts "just sign in" (Option 1).
# Set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET when building so the binary uses
# the embedded client; do not commit these to the repo.
GO_GOOGLE_OAUTH_LDFLAGS :=
ifdef GOOGLE_CLIENT_ID
GO_GOOGLE_OAUTH_LDFLAGS += -X 'github.com/stackgenhq/genie/pkg/tools/google/oauth.GoogleClientID=$(GOOGLE_CLIENT_ID)'
endif
ifdef GOOGLE_CLIENT_SECRET
GO_GOOGLE_OAUTH_LDFLAGS += -X 'github.com/stackgenhq/genie/pkg/tools/google/oauth.GoogleClientSecret=$(GOOGLE_CLIENT_SECRET)'
endif
# Backward compatibility: GOOGLE_CALENDAR_* also inject into shared package.
ifdef GOOGLE_CALENDAR_CLIENT_ID
GO_GOOGLE_OAUTH_LDFLAGS += -X 'github.com/stackgenhq/genie/pkg/tools/google/oauth.GoogleClientID=$(GOOGLE_CALENDAR_CLIENT_ID)'
endif
ifdef GOOGLE_CALENDAR_CLIENT_SECRET
GO_GOOGLE_OAUTH_LDFLAGS += -X 'github.com/stackgenhq/genie/pkg/tools/google/oauth.GoogleClientSecret=$(GOOGLE_CALENDAR_CLIENT_SECRET)'
endif

GO_BUILD_FLAGS=-ldflags="-s -w \
	-X 'github.com/stackgenhq/genie/pkg/config.Version=${GIT_VERSION}' \
	-X 'github.com/stackgenhq/genie/pkg/config.BuildDate=$(shell date +%D)' \
	$(GO_GOOGLE_OAUTH_LDFLAGS)" \
	-mod=mod
DIST_DIR=build

# Go related variables
GO_CMD=go
GO_BUILD=$(GO_CMD) build
GO_CLEAN=$(GO_CMD) clean
GO_TEST=$(GO_CMD) test
GO_MOD=$(GO_CMD) mod
# ------------------------------ setup commands ------------------------------

setup: clean deps generate go/tv ## Setup the environment

go/tv:
	@go mod tidy
	@go mod vendor


.PHONY: deps
deps:
	@which -a goimports > /dev/null || go install golang.org/x/tools/cmd/goimports@latest
	@which -a golangci-lint > /dev/null || go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8


# ------------------------------ build commands ------------------------------
.PHONY: build
build: only-build ## Build the binary

# Build the binary only
.PHONY: only-build
only-build: 
	@mkdir -p $(DIST_DIR)
	@$(GO_BUILD) $(GO_BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME) .

# Build for multiple platforms
.PHONY: build-all
build-all: setup
	mkdir -p $(DIST_DIR)
	# Linux
	GOOS=linux GOARCH=amd64 $(GO_BUILD) $(GO_BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 .
	GOOS=linux GOARCH=arm64 $(GO_BUILD) $(GO_BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 .
	# macOS
	GOOS=darwin GOARCH=amd64 $(GO_BUILD) $(GO_BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 $(GO_BUILD) $(GO_BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 .
	# Windows
	GOOS=windows GOARCH=amd64 $(GO_BUILD) $(GO_BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-windows-amd64.exe .

# Clean build artifacts
.PHONY: clean
clean:
	$(GO_CLEAN)
	rm -f $(BINARY_NAME)
	rm -rf $(DIST_DIR)


# ------------------------------ autogen commands ------------------------------

.PHONY: generate
generate: ## Generate code (if needed)
	$(GO_CMD) generate ./...


# ------------------------------ test commands ------------------------------

test: test/unit ## Run tests

test/unit: ## Run unit tests
	go tool ginkgo ${ARGS} -mod=mod --race -r --junit-report=testreports/report.xml --cover --coverprofile=coverage.out

# ------------------------------ lint commands ------------------------------

lint: deps ## Run linter
	golangci-lint run -v $(ARGS);

lint/fix:
	golangci-lint run -v $(ARGS) --fix

fmt: deps
	@if [ -n "$$(go fmt ./...)" ]; then \
		echo "Go files must be formatted with gofmt. Run 'make fmt/fix' to format your code."; \
		exit 1; \
	fi

fmt/fix:
	gofmt -w .

# Install the binary
.PHONY: install
install:
	$(GO_CMD) install \
		-ldflags="-X 'github.com/stackgenhq/genie/cmd.Version=${GIT_VERSION}' -X 'github.com/stackgenhq/genie/cmd.BuildDate=$(shell date +%D)'" \
		.

# Run the CLI
.PHONY: run
run:
	$(GO_CMD) run .

# ------------------------------ docker commands ------------------------------

DOCKER_BUILD_ARGS=--platform linux/amd64,linux/arm64 \
	--build-arg GIT_VERSION="${GIT_VERSION}" \
	-t ghcr.io/stackgenhq/genie:latest

.PHONY: docker
docker: ## Build the docker image
	docker buildx build $(DOCKER_BUILD_ARGS) .

.PHONY: docker/push
docker/push: ## Build and push the multi-arch docker image
	docker buildx build $(DOCKER_BUILD_ARGS) \
		--push \
		-t ghcr.io/stackgenhq/genie:${GIT_VERSION} .


.PHONY: help
help: ## Display this help message
	@echo "Usage: make <target>"
	@echo ""
	@echo "Targets:"
	@awk -F ':|##' '/^[^\t].+?:.*?##/ { printf "  %-20s %s\n", $$1, $$NF }' $(MAKEFILE_LIST)
