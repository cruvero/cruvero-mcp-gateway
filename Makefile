.PHONY: build test lint vet staticcheck govulncheck gosec dupl godoc-check quality coverage coverage-check test-integration test-security test-load chart-lint chart-render-base chart-render-dev chart-render-staging chart-render-prod chart-validate migrate-up migrate-down docker-build docker-size docker-run clean

VERSION := $(shell git describe --tags --always 2>/dev/null || echo dev)
COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/mcpgw ./cmd/mcpgw

test:
	go test -race -coverprofile=coverage.out ./...

lint:
	golangci-lint run ./...

vet:
	go vet ./...

staticcheck:
	staticcheck ./...

govulncheck:
	govulncheck ./...

gosec:
	./scripts/check-gosec.sh

dupl:
	./scripts/check-dupl.sh

godoc-check:
	./scripts/check-godoc.sh

quality: vet lint staticcheck govulncheck gosec dupl godoc-check coverage-check

coverage:
	go tool cover -func=coverage.out

coverage-check:
	./scripts/check-coverage.sh

test-integration:
	go test -tags integration ./internal/testutil

test-security:
	go test -tags security ./internal/testutil

test-load:
	go test -tags load ./internal/testutil

chart-lint:
	helm lint charts/mcpgateway

chart-render-base:
	helm template charts/mcpgateway -f charts/mcpgateway/values.yaml >/tmp/mcpgateway-base.yaml

chart-render-dev:
	helm template charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-dev.yaml >/tmp/mcpgateway-dev.yaml

chart-render-staging:
	helm template charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-staging.yaml >/tmp/mcpgateway-staging.yaml

chart-render-prod:
	helm template charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-prod.yaml >/tmp/mcpgateway-prod.yaml

chart-validate: chart-lint chart-render-base chart-render-dev chart-render-staging chart-render-prod

migrate-up:
	go run ./cmd/mcpgw migrate --direction up

migrate-down:
	@test "$(CONFIRM)" = "1" || (echo "Set CONFIRM=1 to run down migrations"; exit 1)
	go run ./cmd/mcpgw migrate --direction down

docker-build:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t mcpgw:latest .

docker-size: docker-build
	@echo "Image size:"
	@docker images mcpgw:latest --format '{{.Size}}'

docker-run:
	docker run -p 8443:8443 -p 9090:9090 mcpgw:latest

clean:
	rm -rf bin coverage.out
