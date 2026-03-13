VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: build install clean test lint release snapshot

build:
	go build -ldflags "$(LDFLAGS)" -o bin/yqmev ./cmd/yqmev

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/yqmev

clean:
	rm -rf bin/ dist/

test:
	go test ./...

lint:
	golangci-lint run

release:
	goreleaser release --clean

snapshot:
	goreleaser release --snapshot --clean

# npm package management
npm-publish:
	cd npm && npm publish --access public

npm-update-version:
	cd npm && npm version $(VERSION)
