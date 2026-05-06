# Portico — canonical build / test / lint / preflight commands.
# CI runs the same targets; local dev should use these too.

GO        ?= go
BIN       := bin/portico
MOCKMCP   := bin/mockmcp
LDFLAGS   := -s -w
TAGS      := sqlite_omit_load_extension
COVER_OUT := coverage.out

GO_PRESENT := $(shell test -f go.mod && echo yes || echo no)

.PHONY: help build mockmcp test vet lint clean docker preflight install-hooks check-mirror frontend frontend-check

help:
	@echo "Targets:"
	@echo "  build           Build the portico binary (no-op if no Go code yet)"
	@echo "  mockmcp         Build the standalone mock MCP server (Phase 1+)"
	@echo "  frontend        npm ci + npm run build inside web/console/"
	@echo "  frontend-check  npm run check + npm run lint inside web/console/"
	@echo "  test            Run go test with race detector"
	@echo "  vet             go vet"
	@echo "  lint            golangci-lint run"
	@echo "  preflight       Build, boot dev server, run smoke tests, tear down"
	@echo "  install-hooks   Install git hooks from .githooks/"
	@echo "  check-mirror    Verify AGENTS.md == CLAUDE.md (verbatim invariant)"
	@echo "  docker          Build Docker image"
	@echo "  clean           Remove build artifacts"

build:
ifeq ($(GO_PRESENT),yes)
	@mkdir -p bin
	CGO_ENABLED=0 $(GO) build -tags '$(TAGS)' -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/portico
else
	@echo "build: go.mod absent — skipping (pre-Go-code)"
endif

mockmcp:
ifeq ($(GO_PRESENT),yes)
	@if [ -d examples/servers/mock/cmd/mockmcp ]; then \
		mkdir -p bin; \
		CGO_ENABLED=0 $(GO) build -ldflags '$(LDFLAGS)' -o $(MOCKMCP) ./examples/servers/mock/cmd/mockmcp; \
	else \
		echo "mockmcp: examples/servers/mock/cmd/mockmcp absent — skipping"; \
	fi
else
	@echo "mockmcp: go.mod absent — skipping (pre-Go-code)"
endif

test:
ifeq ($(GO_PRESENT),yes)
	$(GO) test -race -coverprofile=$(COVER_OUT) -covermode=atomic ./...
else
	@echo "test: go.mod absent — skipping (pre-Go-code)"
endif

vet:
ifeq ($(GO_PRESENT),yes)
	$(GO) vet ./...
else
	@echo "vet: go.mod absent — skipping (pre-Go-code)"
endif

lint:
ifeq ($(GO_PRESENT),yes)
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not found; install from https://golangci-lint.run"; exit 1; }
	golangci-lint run ./...
else
	@echo "lint: go.mod absent — skipping (pre-Go-code)"
endif

preflight: check-mirror
	@bash scripts/preflight.sh

install-hooks:
	@bash scripts/install-hooks.sh

check-mirror:
	@if ! diff -q AGENTS.md CLAUDE.md >/dev/null 2>&1; then \
		echo "ERROR: AGENTS.md and CLAUDE.md must be verbatim identical."; \
		diff -u AGENTS.md CLAUDE.md || true; \
		exit 1; \
	fi
	@echo "check-mirror: AGENTS.md == CLAUDE.md OK"

docker:
ifeq ($(GO_PRESENT),yes)
	docker build -t portico/portico:dev .
else
	@echo "docker: go.mod absent — skipping (pre-Go-code)"
endif

frontend:
	@if [ -f web/console/package.json ]; then \
		cd web/console && npm ci && npm run build; \
	else \
		echo "frontend: web/console/package.json absent — skipping"; \
	fi

frontend-check:
	@if [ -f web/console/package.json ]; then \
		cd web/console && npm ci && npm run check && npm run lint; \
	else \
		echo "frontend-check: web/console/package.json absent — skipping"; \
	fi

clean:
	rm -rf bin $(COVER_OUT) coverage.html web/console/build/_app web/console/build/index.html web/console/build/favicon.svg
	@find web/console/build -type f ! -name '.gitkeep' -delete 2>/dev/null || true
