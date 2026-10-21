.PHONY: build test lint install clean

BIN_EXT := $(if $(filter windows,$(shell go env GOOS)),.exe,)

build:
	go build -trimpath -o bin/airtable-pp-cli$(BIN_EXT) ./cmd/airtable-pp-cli

test:
	go test ./...

lint:
	golangci-lint run

install:
	go install ./cmd/airtable-pp-cli

clean:
	rm -rf bin/

build-mcp:
	go build -trimpath -o bin/airtable-pp-mcp$(BIN_EXT) ./cmd/airtable-pp-mcp

install-mcp:
	go install ./cmd/airtable-pp-mcp

build-all: build build-mcp
