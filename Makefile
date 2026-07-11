SHELL := /usr/bin/env bash
GO ?= go
BINARY := servervault
CMD_DIR := ./cmd/servervault
DIST_DIR := dist

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# TODO(internal/version): main.Version/Commit/Date are set directly on the
# `main` package (see cmd/servervault/main.go) because there is no
# internal/version package yet. Once one exists (see ROADMAP.md v0.2.0-alpha
# "build metadata"), move these -X targets to internal/version.Version/
# Commit/Date and update the ldflags in .github/workflows/release.yml too.
LDFLAGS := -s -w \
	-X main.Version=$(VERSION) \
	-X main.Commit=$(COMMIT) \
	-X main.Date=$(DATE)

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
