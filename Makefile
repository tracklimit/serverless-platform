.PHONY: dev test test-race lint fmt migrate-up migrate-down migrate-create check

# ---- Local Development ----

dev: ## Run the API in development mode
	ENVIRONMENT=development go run ./cmd/api

# ---- Testing ----

test: ## Run all tests
	go test ./... -count=1

test-race: ## Run all tests with race detector
	go test ./... -race -count=1

test-cover: ## Run tests with coverage report
	go test ./... -coverprofile=coverage.out -count=1
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# ---- Code Quality ----

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format code
	gofmt -s -w .
	goimports -w .

check: lint test ## Run lint + test (CI gate)

# ---- Database Migrations ----

migrate-up: ## Apply all pending migrations
	migrate -path migrations -database "$(DATABASE_URL)" up

migrate-down: ## Rollback the last migration
	migrate -path migrations -database "$(DATABASE_URL)" down 1

migrate-create: ## Create a new migration (usage: make migrate-create name=add_deployments)
	migrate create -ext sql -dir migrations -seq $(name)