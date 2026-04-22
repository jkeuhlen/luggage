SHELL := /bin/bash

BIN_DIR := $(CURDIR)/.bin
export PATH := $(BIN_DIR):$(PATH)

GO_FILES := $(shell find . -type f -name '*.go' -not -path './.git/*' -not -path './.bin/*')

GOIMPORTS := $(BIN_DIR)/goimports
STATICCHECK := $(BIN_DIR)/staticcheck
GOLANGCI_LINT := $(BIN_DIR)/golangci-lint
GOVULNCHECK := $(BIN_DIR)/govulncheck

GOIMPORTS_PKG := golang.org/x/tools/cmd/goimports@v0.31.0
STATICCHECK_PKG := honnef.co/go/tools/cmd/staticcheck@v0.6.1
GOLANGCI_LINT_PKG := github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
GOVULNCHECK_PKG := golang.org/x/vuln/cmd/govulncheck@v1.1.4

COVERAGE_MIN ?= 30

.PHONY: tools fmt fmt-check vet staticcheck lint build test test-race vuln mod-tidy-check coverage-check check ci clean-tools

$(BIN_DIR):
	@mkdir -p $(BIN_DIR)

$(GOIMPORTS): | $(BIN_DIR)
	@echo "installing goimports"
	@GOBIN=$(BIN_DIR) go install $(GOIMPORTS_PKG)

$(STATICCHECK): | $(BIN_DIR)
	@echo "installing staticcheck"
	@GOBIN=$(BIN_DIR) go install $(STATICCHECK_PKG)

$(GOLANGCI_LINT): | $(BIN_DIR)
	@echo "installing golangci-lint"
	@GOBIN=$(BIN_DIR) go install $(GOLANGCI_LINT_PKG)

$(GOVULNCHECK): | $(BIN_DIR)
	@echo "installing govulncheck"
	@GOBIN=$(BIN_DIR) go install $(GOVULNCHECK_PKG)

tools: $(GOIMPORTS) $(STATICCHECK) $(GOLANGCI_LINT) $(GOVULNCHECK)

fmt: $(GOIMPORTS)
	@gofmt -w $(GO_FILES)
	@$(GOIMPORTS) -w $(GO_FILES)

fmt-check: $(GOIMPORTS)
	@out="$$(gofmt -l $(GO_FILES))"; \
	if [[ -n "$$out" ]]; then \
		echo "gofmt differences found:"; \
		echo "$$out"; \
		exit 1; \
	fi
	@out="$$($(GOIMPORTS) -l $(GO_FILES))"; \
	if [[ -n "$$out" ]]; then \
		echo "goimports differences found:"; \
		echo "$$out"; \
		exit 1; \
	fi

mod-tidy-check:
	@go mod tidy -diff

vet:
	@go vet ./...

staticcheck: $(STATICCHECK)
	@$(STATICCHECK) ./...

lint: $(GOLANGCI_LINT)
	@$(GOLANGCI_LINT) run ./...

build:
	@go build ./...

test:
	@go test ./...

test-race:
	@go test -race ./...

vuln: $(GOVULNCHECK)
	@$(GOVULNCHECK) ./...

coverage-check:
	@go test -coverprofile=coverage.out ./...
	@total="$$(go tool cover -func=coverage.out | awk '/^total:/ {gsub(/%/, "", $$3); print $$3}')"; \
	echo "coverage: $$total% (min $(COVERAGE_MIN)%)"; \
	awk -v total="$$total" -v min="$(COVERAGE_MIN)" 'BEGIN { if (total+0 < min+0) exit 1 }' || \
		( echo "coverage threshold not met"; rm -f coverage.out; exit 1 )
	@rm -f coverage.out

check: fmt-check mod-tidy-check vet staticcheck lint build test test-race vuln coverage-check

ci: tools check

clean-tools:
	@rm -rf $(BIN_DIR)
