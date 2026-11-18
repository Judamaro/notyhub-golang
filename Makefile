# Go runs inside Docker/Podman, so no local Go toolchain is needed.
GO_IMAGE ?= docker.io/library/golang:1.26
DOCKER   ?= docker
GO       := $(DOCKER) run --rm -v "$(CURDIR)":/src:Z -w /src -v notyhub-gomod:/go/pkg/mod -v notyhub-gocache:/root/.cache/go-build $(GO_IMAGE)

.PHONY: tidy fmt vet test build run up down

tidy: ## Resolve dependencies (go.mod / go.sum)
	$(GO) go mod tidy

fmt: ## Format the code
	$(GO) gofmt -w .

vet: ## Static checks
	$(GO) go vet ./...

test: ## Run the tests with the race detector
	$(GO) go test -race -count=1 ./...

build: ## Build the binary into bin/
	$(GO) sh -c 'CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/notyhub ./cmd/notyhub'

run: ## Run locally in development (API :8080, metrics :9465)
	$(DOCKER) run --rm -it -p 8080:8080 -p 9465:9465 -e APP_ENV=development -v "$(CURDIR)":/src:Z -w /src -v notyhub-gomod:/go/pkg/mod -v notyhub-gocache:/root/.cache/go-build $(GO_IMAGE) go run ./cmd/notyhub

up: ## Build the image and start the container
	$(DOCKER) compose up -d --build

down: ## Stop the container
	$(DOCKER) compose down
