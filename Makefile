.PHONY: build test clean run run-http install lint help

GOPATH=$(shell pwd)
GO=~/go-sdk/bin/go

build:
	$(GO) build -o openpool ./cmd/node2

test:
	$(GO) test ./pkg/... -v -cover

test-verbose:
	$(GO) test ./pkg/... -v -cover -race

test-coverage:
	$(GO) test ./pkg/... -coverprofile=coverage.out
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

clean:
	rm -f openpool openpool.db peerstore.json
	rm -rf coverage.out coverage.html

run:
	./openpool -http 8080 -port 9000 -wasm wasm/sandbox.wasm -dht

run-test:
	./openpool -test -wasm wasm/sandbox.wasm

run-market:
	./openpool -http 8080 -port 9000 -wasm wasm/sandbox.wasm -market -dht

run-gpu:
	./openpool -http 8080 -port 9000 -wasm wasm/sandbox.wasm -gpu -dht

install:
	$(GO) mod download
	$(GO) mod tidy

lint:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

check: fmt lint test

help:
	@echo "OpenPool Makefile Commands:"
	@echo "  make build       - Build the openpool binary"
	@echo "  make test        - Run all unit tests"
	@echo "  make test-verbose - Run tests with race detection"
	@echo "  make test-coverage - Generate coverage report (coverage.html)"
	@echo "  make clean        - Remove binaries and temp files"
	@echo "  make run          - Start node with HTTP API"
	@echo "  make run-test     - Run built-in test task"
	@echo "  make run-market   - Start node with marketplace enabled"
	@echo "  make run-gpu      - Start node with GPU support"
	@echo "  make install      - Download Go dependencies"
	@echo "  make lint         - Run go vet"
	@echo "  make fmt          - Format code"
	@echo "  make check        - Format, lint, and test"