.PHONY: build test lint install clean

BIN_EXT := $(if $(filter windows,$(shell go env GOOS)),.exe,)

build:
	go build -trimpath -o bin/pingenerator-pp-cli$(BIN_EXT) ./cmd/pingenerator-pp-cli

test:
	go test ./...

lint:
	golangci-lint run

install:
	go install ./cmd/pingenerator-pp-cli

clean:
	rm -rf bin/

build-mcp:
	go build -trimpath -o bin/pingenerator-pp-mcp$(BIN_EXT) ./cmd/pingenerator-pp-mcp

install-mcp:
	go install ./cmd/pingenerator-pp-mcp

build-all: build build-mcp
