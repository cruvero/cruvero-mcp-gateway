.PHONY: build test lint vet coverage migrate-up migrate-down clean

build:
	go build -trimpath -ldflags="-s -w" -o bin/mcpgw ./cmd/mcpgw

test:
	go test -race -coverprofile=coverage.out ./...

lint:
	golangci-lint run ./...

vet:
	go vet ./...

coverage:
	go tool cover -func=coverage.out

migrate-up:
	@echo "TODO: implement migrations up command"

migrate-down:
	@echo "TODO: implement migrations down command"

clean:
	rm -rf bin coverage.out
