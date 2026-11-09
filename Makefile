GO ?= go
.PHONY: build frontend test run
frontend:
	cd web && npm ci && npm run build
build:
	$(GO) build -buildvcs=false -trimpath -ldflags='-s -w' -o bin/navtool ./cmd/navtool
test:
	$(GO) test ./...
run:
	$(GO) run ./cmd/navtool
