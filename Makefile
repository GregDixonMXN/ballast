GO := go
LINT := golangci-lint

.PHONY: dev test lint build tidy demo

dev:
	bash scripts/dev.sh

test:
	$(GO) test ./...

lint:
	$(GO) vet ./...
	@if command -v $(LINT) >/dev/null 2>&1; then $(LINT) run ./...; fi

build:
	$(GO) build ./...

tidy:
	$(GO) mod tidy

demo:
	bash scripts/demo.sh
