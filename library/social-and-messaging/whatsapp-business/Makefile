.PHONY: build test lint install clean

BIN_EXT := $(if $(filter windows,$(shell go env GOOS)),.exe,)

build:
	go build -trimpath -o bin/whatsapp-business-pp-cli$(BIN_EXT) ./cmd/whatsapp-business-pp-cli

test:
	go test ./...

lint:
	golangci-lint run

install:
	go install ./cmd/whatsapp-business-pp-cli

clean:
	rm -rf bin/

build-mcp:
	go build -trimpath -o bin/whatsapp-business-pp-mcp$(BIN_EXT) ./cmd/whatsapp-business-pp-mcp

install-mcp:
	go install ./cmd/whatsapp-business-pp-mcp

build-all: build build-mcp
