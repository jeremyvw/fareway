BINARY := bin/fareway
PKG    := ./...

.PHONY: help run build test cover vet fmt tidy check clean

help: ## Show available targets
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  \033[36m%-8s\033[0m %s\n", $$1, $$2}'

run: ## Run the API server
	go run ./cmd

build: ## Compile the server binary into bin/
	go build -o $(BINARY) ./cmd

test: ## Run all tests with the race detector
	go test $(PKG) -race -cover

cover: ## Run tests and open an HTML coverage report
	go test $(PKG) -race -coverprofile=coverage.out
	go tool cover -html=coverage.out

vet: ## Run go vet
	go vet $(PKG)

fmt: ## Format all Go source
	go fmt $(PKG)

tidy: ## Tidy module dependencies
	go mod tidy

check: fmt vet test ## Format, vet and test — run this before committing

clean: ## Remove build and coverage artifacts
	rm -rf bin coverage.out
