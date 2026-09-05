.PHONY: build test lint clean fmt tidy ci help

GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-dev")
LDFLAGS ?= -s -w
BINARY ?= downloader-native-usenet

build:
	$(GO) build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/module

test:
	$(GO) test -race -count=1 -timeout 60s ./...

lint:
	golangci-lint run --timeout 120s ./...

clean:
	rm -f $(BINARY)
	rm -f cmd/module/module

fmt:
	$(GO) fmt ./...

tidy:
	$(GO) mod tidy

ci: test build

help:
	@echo "Targets: build test lint clean fmt tidy ci"
