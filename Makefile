.PHONY: build test test-verbose test-coverage clean run run-http run-market install lint fmt help

BINARY=openpool
CMD=./cmd/integrated
CGO_ENABLED=1

build:
	CGO_ENABLED=$(CGO_ENABLED) go build -o $(BINARY) $(CMD)

test:
	CGO_ENABLED=$(CGO_ENABLED) go test ./pkg/... -v -cover

test-verbose:
	CGO_ENABLED=$(CGO_ENABLED) go test ./pkg/... -v -cover -race

test-coverage:
	CGO_ENABLED=$(CGO_ENABLED) go test ./pkg/... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

clean:
	rm -f $(BINARY) openpool.db peerstore.json
	rm -f coverage.out coverage.html

run: build
	./$(BINARY) -http 8080 -port 9000 -wasm wasm -dht

run-test: build
	./$(BINARY) -test

run-market: build
	./$(BINARY) -http 8080 -port 9000 -wasm wasm -dht

install:
	go mod download
	go mod tidy

lint:
	go vet ./...

fmt:
	go fmt ./...

check: fmt lint test

help:
	@echo "OpenPool Makefile Commands:"
	@echo "  make build          - Build the openpool binary"
	@echo "  make test           - Run all unit tests"
	@echo "  make test-verbose   - Run tests with race detection"
	@echo "  make test-coverage  - Generate coverage report (coverage.html)"
	@echo "  make clean          - Remove binaries and temp files"
	@echo "  make run            - Start node with HTTP API"
	@echo "  make run-test       - Run built-in test task"
	@echo "  make run-market     - Start node with marketplace enabled"
	@echo "  make install        - Download Go dependencies"
	@echo "  make lint           - Run go vet"
	@echo "  make fmt            - Format code"
	@echo "  make check          - Format, lint, and test"