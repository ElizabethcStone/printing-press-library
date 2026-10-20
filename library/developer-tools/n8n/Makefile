.PHONY: build test lint install clean

BIN_EXT := $(if $(filter windows,$(shell go env GOOS)),.exe,)

build:
	go build -trimpath -o bin/n8n-pp-cli$(BIN_EXT) ./cmd/n8n-pp-cli

test:
	go test ./...

lint:
	golangci-lint run

install:
	go install ./cmd/n8n-pp-cli

clean:
	rm -rf bin/

build-mcp:
	go build -trimpath -o bin/n8n-pp-mcp$(BIN_EXT) ./cmd/n8n-pp-mcp

install-mcp:
	go install ./cmd/n8n-pp-mcp

build-all: build build-mcp
