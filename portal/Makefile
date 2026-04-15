.PHONY: web-deps web-build go-build build clean

VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

web-deps:
	cd web && npm install

web-build: web-deps
	cd web && npm run build

go-build: web-build
	go build -ldflags "$(LDFLAGS)" -o bin/lingtai-portal .

build: go-build

cross-compile: web-build
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/lingtai-portal-darwin-arm64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/lingtai-portal-darwin-x64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/lingtai-portal-linux-x64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/lingtai-portal-linux-arm64 .

clean:
	rm -rf bin/ web/dist/
