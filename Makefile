BINARY := spec
BINDIR ?= $(HOME)/.local/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X github.com/aaronl1011/spec/cmd.Version=$(VERSION)"
GOFLAGS := -trimpath
DETECTED_SHELL := $(notdir $(shell echo $$SHELL))

.PHONY: build test lint lint-strict lint-install deadcode clean install install-completions fmt vet docs install-man

build:
	go build $(GOFLAGS) $(LDFLAGS) -o bin/$(BINARY) .

install:
	mkdir -p $(BINDIR)
	go build $(GOFLAGS) $(LDFLAGS) -o $(BINDIR)/$(BINARY) .
	@echo "Installed $(BINDIR)/$(BINARY)"
	@echo "If the shell cannot find spec, add $(BINDIR) to PATH (fish: fish_add_path $(BINDIR))"

docs:
	go run ./tools/gen-man --output docs/man

install-man: docs
	mkdir -p /usr/local/share/man/man1
	cp docs/man/spec*.1 /usr/local/share/man/man1/
	mandb 2>/dev/null || true

install-completions:
	@echo "Detected shell: $(DETECTED_SHELL)"
	@case "$(DETECTED_SHELL)" in \
	zsh) \
		mkdir -p "$(HOME)/.zfunc" && \
		$(BINDIR)/$(BINARY) completion zsh > "$(HOME)/.zfunc/_$(BINARY)" && \
		grep -qF 'fpath=(~/.zfunc' "$(HOME)/.zshrc" 2>/dev/null || \
		  printf '\nfpath=(~/.zfunc $$fpath)\nautoload -U compinit; compinit\n' >> "$(HOME)/.zshrc" && \
		echo "Zsh completions installed to $(HOME)/.zfunc/_$(BINARY) — restart your shell or run: source ~/.zshrc" ;; \
	bash) \
		mkdir -p "$(HOME)/.local/share/bash-completion/completions" && \
		$(BINDIR)/$(BINARY) completion bash > "$(HOME)/.local/share/bash-completion/completions/$(BINARY)" && \
		echo "Bash completions installed — restart your shell or run: source ~/.bashrc" ;; \
	fish) \
		mkdir -p "$(HOME)/.config/fish/completions" && \
		$(BINDIR)/$(BINARY) completion fish > "$(HOME)/.config/fish/completions/$(BINARY).fish" && \
		echo "Fish completions installed to $(HOME)/.config/fish/completions/$(BINARY).fish — active immediately" ;; \
	*) \
		echo "Unsupported shell: $(DETECTED_SHELL). Run 'spec completion [bash|zsh|fish|powershell]' manually." >&2 ; \
		exit 1 ;; \
	esac

test:
	go test ./... -race -count=1

test-cover:
	go test ./... -race -count=1 -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

# Version must match .github/workflows/ci.yaml (golangci-lint-action `version`).
GOLANGCI_LINT_VERSION := v2.12.2

lint:
	go vet ./...
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed, skipping"

# lint-strict reproduces the CI lint job exactly and fails hard if the linter is
# missing. Run this before opening a PR. CI runs the same command + version.
lint-strict:
	go vet ./...
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed — run 'make lint-install'"; exit 1; }
	golangci-lint run --timeout=5m

lint-install:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

# Pinned to match the CI deadcode job.
DEADCODE_VERSION := v0.39.0

# deadcode fails the build if any function is unreachable from main OR from the
# test surface (-test). Test helpers such as store.OpenMemory are intentionally
# treated as live. A non-zero exit means there is dead code to remove.
deadcode:
	@out=$$(go run golang.org/x/tools/cmd/deadcode@$(DEADCODE_VERSION) -test ./...); \
	if [ -n "$$out" ]; then echo "$$out"; echo "dead code found — remove it or wire it up"; exit 1; fi; \
	echo "no dead code"

fmt:
	gofmt -s -w .

vet:
	go vet ./...

clean:
	rm -rf bin/ coverage.out coverage.html

all: lint deadcode test build
