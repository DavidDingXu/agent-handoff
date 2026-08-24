# agent-handoff — build, test, release
BINARY  := agent-handoff
MODULE  := github.com/DavidDingXu/agent-handoff
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X github.com/DavidDingXu/agent-handoff/internal/cli.Version=$(VERSION)

GO      ?= go
TARGETS := darwin-amd64 darwin-arm64 linux-amd64 linux-arm64 windows-amd64

.PHONY: all build test vet fmt lint clean release install cross dist check help

all: build

## build: compile the CLI for the host platform
build:
	$(GO) build -trimpath -buildvcs=false -ldflags "$(LDFLAGS)" -o bin/$(BINARY) .

## test: run the full test suite
test:
	$(GO) test ./... -count=1

## vet: static analysis
vet:
	$(GO) vet ./...

## fmt: format all Go sources
fmt:
	$(GO) fmt ./...

## lint: golangci-lint (install: https://golangci-lint.run)
lint:
	golangci-lint run

## check: everything CI runs
check: fmt vet test

## cross: build all release targets into bin/<os>-<arch>/
cross:
	@for target in $(TARGETS); do \
		os=$${target%-*}; arch=$${target##*-}; \
		ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
		echo "==> $$target"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			$(GO) build -trimpath -buildvcs=false -ldflags "$(LDFLAGS)" \
			-o bin/$$target/$(BINARY)$$ext . || exit 1; \
	done

## dist: cross-compile + package release archives into dist/
dist: cross
	@rm -rf dist && mkdir -p dist
	@for target in $(TARGETS); do \
		os=$${target%-*}; arch=$${target##*-}; \
		ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
		if command -v tar >/dev/null && [ "$$os" != "windows" ]; then \
			tar -czf dist/$(BINARY)-$(VERSION)-$$os-$$arch.tar.gz \
				-C bin/$$target $(BINARY)$$ext || exit 1; \
		else \
			(cd bin/$$target && zip -q ../../dist/$(BINARY)-$(VERSION)-$$os-$$arch.zip $(BINARY)$$ext) || exit 1; \
		fi; \
	done
	@shasum -a 256 dist/* > dist/checksums.txt 2>/dev/null || \
		sha256sum dist/* > dist/checksums.txt
	@echo "dist:" && ls dist

## release: full local release build via goreleaser (snapshot)
release:
	goreleaser release --snapshot --clean

## install: install the CLI into GOPATH/bin
install:
	$(GO) install -trimpath -buildvcs=false -ldflags "$(LDFLAGS)" .

## clean: remove build artifacts
clean:
	rm -rf bin/$(BINARY) dist

help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'
