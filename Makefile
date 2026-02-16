.PHONY: help init build test clean proto docker-build docker-up docker-down

# Colors for output
GREEN  := $(shell tput -Txterm setaf 2)
YELLOW := $(shell tput -Txterm setaf 3)
WHITE  := $(shell tput -Txterm setaf 7)
RESET  := $(shell tput -Txterm sgr0)

help: ## Show this help message
	@echo ''
	@echo 'Usage:'
	@echo '  ${YELLOW}make${RESET} ${GREEN}<target>${RESET}'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  ${YELLOW}%-20s${GREEN}%s${RESET}\n", $$1, $$2}' $(MAKEFILE_LIST)

init: ## Initialize project dependencies
	@echo "${GREEN}Initializing Go module...${RESET}"
	cd gateway && go mod download
	@echo "${GREEN}Initializing Python environment...${RESET}"
	cd ai-service && python -m venv venv && \
		. venv/bin/activate && pip install -r requirements.txt

proto: ## Generate gRPC code from proto files
	@echo "${GREEN}Generating gRPC code...${RESET}"
	cd shared/proto && \
		protoc --go_out=../../gateway/pkg/pb --go_opt=paths=source_relative \
		       --go-grpc_out=../../gateway/pkg/pb --go-grpc_opt=paths=source_relative \
		       ai_service.proto
	cd shared/proto && \
		python3 -m grpc_tools.protoc -I. \
		       --python_out=../../ai-service/src/generated \
		       --grpc_python_out=../../ai-service/src/generated \
		       ai_service.proto

build: proto ## Build all services
	@echo "${GREEN}Building Gateway service...${RESET}"
	cd gateway && go build -o gateway ./cmd/gateway
	@echo "${GREEN}Gateway built successfully${RESET}"

test: ## Run tests
	@echo "${GREEN}Running Gateway tests...${RESET}"
	cd gateway && go test -v -race -coverprofile=coverage.out ./...
	@echo "${GREEN}Running AI Service tests...${RESET}"
	cd ai-service && . venv/bin/activate && pytest -v

clean: ## Clean build artifacts
	@echo "${YELLOW}Cleaning build artifacts...${RESET}"
	rm -f gateway/gateway
	rm -rf ai-service/dist
	rm -f gateway/coverage.out
	rm -rf gateway/pkg/pb
	rm -rf ai-service/src/generated

docker-build: ## Build Docker images
	@echo "${GREEN}Building Docker images...${RESET}"
	docker-compose build

docker-up: ## Start services with Docker Compose
	@echo "${GREEN}Starting services...${RESET}"
	docker-compose up -d

docker-down: ## Stop services
	@echo "${YELLOW}Stopping services...${RESET}"
	docker-compose down

docker-logs: ## Show service logs
	docker-compose logs -f

run-gateway: build ## Run Gateway service locally
	@echo "${GREEN}Starting Gateway service...${RESET}"
	cd gateway && ./gateway

run-ai-service: ## Run AI Service locally
	@echo "${GREEN}Starting AI Service...${RESET}"
	cd ai-service && . venv/bin/activate && python -m src.main

lint: ## Run linters
	@echo "${GREEN}Linting Go code...${RESET}"
	cd gateway && go vet ./...
	cd gateway && golangci-lint run || true
	@echo "${GREEN}Linting Python code...${RESET}"
	cd ai-service && . venv/bin/activate && \
		black --check src/ && \
		ruff check src/

fmt: ## Format code
	@echo "${GREEN}Formatting Go code...${RESET}"
	cd gateway && go fmt ./...
	@echo "${GREEN}Formatting Python code...${RESET}"
	cd ai-service && . venv/bin/activate && black src/

.DEFAULT_GOAL := help
