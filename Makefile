export CGO_ENABLED=0
GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
.PHONY: build test test-darwin agent run dmg
build:
	$(GO) build ./...
test:            # Linux + portable packages (run on agent-box)
	$(GO) test ./...
test-darwin:     # macOS-only packages (run on the Mac)
	$(GO) test ./collect/darwin/... ./internal/cf/... ./gpu/apple/... ./power/... ./mac/... ./ui/...
agent:
	GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o dist/hotwatch-agent-linux-amd64 ./cmd/hotwatch-agent
	GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o dist/hotwatch-agent-linux-arm64 ./cmd/hotwatch-agent
run:
	$(GO) run ./cmd/hotwatch
dmg:
	$(GO) tool mygo build
