.PHONY: help build test clean install lint fmt

# Colors
GREEN  := $(shell tput -Txterm setaf 2)
YELLOW := $(shell tput -Txterm setaf 3)
RESET  := $(shell tput -Txterm sgr0)

help: ## Show this help message
	@echo ''
	@echo 'Usage:'
	@echo '  ${YELLOW}make${RESET} ${GREEN}<target>${RESET}'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  ${YELLOW}%-20s${GREEN}%s${RESET}\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build ngoclaw binary
	@echo "${GREEN}Building ngoclaw...${RESET}"
	cd gateway && go build -o bin/ngoclaw ./cmd/cli
	@echo "${GREEN}Build complete → gateway/bin/ngoclaw${RESET}"

install: build ## Install ngoclaw to /usr/local/bin
	@echo "${GREEN}Installing ngoclaw...${RESET}"
	sudo ln -sf $(shell pwd)/gateway/bin/ngoclaw /usr/local/bin/ngoclaw
	@echo "${GREEN}Installed: /usr/local/bin/ngoclaw${RESET}"

test: ## Run tests
	@echo "${GREEN}Running tests...${RESET}"
	cd gateway && go test ./...

test-race: ## Run tests with race detector
	@echo "${GREEN}Running tests with -race...${RESET}"
	cd gateway && go test -v -race -coverprofile=coverage.out ./...

lint: ## Run linters
	@echo "${GREEN}Linting...${RESET}"
	cd gateway && go vet ./...
	cd gateway && golangci-lint run || true

fmt: ## Format code
	@echo "${GREEN}Formatting...${RESET}"
	cd gateway && go fmt ./...

clean: ## Clean build artifacts
	@echo "${YELLOW}Cleaning...${RESET}"
	rm -f gateway/bin/ngoclaw
	rm -f gateway/coverage.out

.DEFAULT_GOAL := help
