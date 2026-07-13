SHELL := /usr/bin/env bash
GO ?= go
BINARY := servervault
CMD_DIR := ./cmd/servervault
DIST_DIR := dist

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

VERSION_PKG := github.com/JamunaSoft/servervault/internal/version

LDFLAGS := -s -w \
	-X $(VERSION_PKG).Version=$(VERSION) \
	-X $(VERSION_PKG).Commit=$(COMMIT) \
	-X $(VERSION_PKG).Date=$(DATE)

.PHONY: all
all: fmt vet test build

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: fmt-check
fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [[ -n "$$unformatted" ]]; then \
		echo "Not gofmt-formatted:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: test
test:
	$(GO) test -race -cover ./...

# Real restic/pg_dump/pg_restore/psql against temporary local
# repositories/databases. Skips cleanly per-test when a prerequisite is
# missing locally; see docs/testing.md.
.PHONY: test-integration
test-integration:
	$(GO) test -tags=integration -race ./...

# Opt-in, best-effort probe of restic's own lock-conflict behavior against
# a real repository. Not part of `test-integration`; version-sensitive.
# See docs/testing.md and internal/restic/lockprobe_test.go.
.PHONY: test-resticlock
test-resticlock:
	$(GO) test -tags=integration,resticlock -race ./internal/restic/...

.PHONY: build
build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD_DIR)

.PHONY: install
install: build
	install -m 755 $(BINARY) /usr/local/bin/$(BINARY)

.PHONY: clean
clean:
	rm -f $(BINARY)
	rm -rf $(DIST_DIR)

.PHONY: shellcheck
shellcheck:
	bash -n bin/* install.sh
	shellcheck bin/* install.sh

.PHONY: verify
verify: fmt-check vet test build shellcheck

.PHONY: run
run: build
	./$(BINARY) $(ARGS)
