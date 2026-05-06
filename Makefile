BINARY  := ccsm
PKG     := ./cmd/ccsm
OUT     := bin/$(BINARY)
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PREFIX  ?= $(or $(GOBIN),$(shell go env GOBIN),$(shell go env GOPATH)/bin)

.PHONY: all build run install uninstall test vet fmt tidy clean help

all: build

build: ## compile binary into bin/
	@mkdir -p bin
	go build -ldflags '$(LDFLAGS)' -o $(OUT) $(PKG)

run: ## run TUI without persisting a binary
	go run $(PKG)

list: ## run -list mode
	go run $(PKG) -list

install: ## install to $$GOBIN (or $$GOPATH/bin)
	go install -ldflags '$(LDFLAGS)' $(PKG)
	@echo "installed: $(PREFIX)/$(BINARY)"

uninstall: ## remove installed binary
	rm -f $(PREFIX)/$(BINARY)

test: ## run tests
	go test ./...

vet: ## go vet
	go vet ./...

fmt: ## gofmt -w .
	gofmt -w .

tidy: ## go mod tidy
	go mod tidy

clean: ## remove build artifacts
	rm -rf bin/

help: ## list targets
	@awk 'BEGIN{FS=":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  %-12s %s\n",$$1,$$2}' $(MAKEFILE_LIST)
