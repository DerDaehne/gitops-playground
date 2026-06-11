# Makefile for the Go port of gitops-playground.
#
# Targets:
#   make tidy   — go mod tidy (pulls indirect transitive deps the first time)
#   make build  — builds the gop binary into ./bin/gop
#   make test   — runs every package's tests
#   make vet    — go vet ./...
#   make lint   — golangci-lint if available, otherwise vet
#   make check  — vet + test
#   make clean  — removes ./bin
#
# All targets assume `go` (>= 1.22) is on PATH.

GO       ?= go
PKG      := ./...
BIN_DIR  := bin
BINARY   := $(BIN_DIR)/gop

VERSION  ?= $(shell git describe --tags --dirty --always 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)

LDFLAGS  := -X 'github.com/cloudogu/gitops-playground/go/internal/cli.Version=$(VERSION)' \
            -X 'github.com/cloudogu/gitops-playground/go/internal/cli.Commit=$(COMMIT)'

.PHONY: tidy build test vet lint check clean

tidy:
	$(GO) mod tidy

build: $(BIN_DIR)
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/gop

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

test:
	$(GO) test -race -count=1 $(PKG)

vet:
	$(GO) vet $(PKG)

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, falling back to go vet"; \
		$(GO) vet $(PKG); \
	fi

check: vet test

clean:
	rm -rf $(BIN_DIR)
