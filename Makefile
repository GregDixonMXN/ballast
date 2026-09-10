GO ?= go
export GO
.PHONY: dev test check build package demo

dev:
	./scripts/dev.sh

test:
	$(GO) test -race ./...

check:
	./scripts/check.sh

build:
	./scripts/build.sh

package:
	./scripts/package.sh

demo:
	./scripts/demo.sh
