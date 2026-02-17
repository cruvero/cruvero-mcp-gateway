.PHONY: build test lint vet coverage coverage-check test-integration test-security test-load migrate-up migrate-down docker-build docker-run clean

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

migrate-up:
	@echo "TODO: implement migrations up command"

migrate-down:
	@echo "TODO: implement migrations down command"

docker-build:
	docker build -t mcpgw:latest .

docker-run:
	docker run -p 8443:8443 -p 9090:9090 mcpgw:latest

clean:
	rm -rf bin coverage.out
