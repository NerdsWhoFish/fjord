BIN := $(HOME)/bin/fjord
VERSION := $(shell git rev-parse HEAD)
export GOWORK := off

.PHONY: build web check test extension install clean
web:
	cd web && npm ci && npm run build

build: web
	go build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/fjord ./cmd/fjord

check: web
	cd web && npm test
	node --test extension/*.test.mjs scripts/*.test.mjs
	go vet ./...
	FJORD_TEST_GOPLS=1 FJORD_TEST_TOFU_LS=1 go test -race ./...
	@test -z "$$(gofmt -l cmd internal assets.go)"

test:
	go test -race ./internal/...

extension:
	node extension/package.mjs artifacts/fjord-chrome-extension.zip

install: check build
	install -m 755 bin/fjord $(BIN)

clean:
	rm -rf bin web/dist
