.PHONY: build dev clean cross-compile

VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/lingtai-tui .

# Fast local rebuild. The Homebrew binary at /opt/homebrew/bin/lingtai-tui is
# already a symlink to bin/lingtai-tui, so `make dev` immediately updates the
# installed TUI. Skips version stamp for speed; tags `dev` build.
dev:
	go build -ldflags "-X main.version=dev" -o bin/lingtai-tui .
	@echo "built bin/lingtai-tui (dev) — run: lingtai-tui"

cross-compile:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/lingtai-darwin-arm64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/lingtai-darwin-x64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/lingtai-linux-x64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/lingtai-linux-arm64 .

clean:
	rm -rf bin/
