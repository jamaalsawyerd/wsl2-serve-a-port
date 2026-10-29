BINARY  := serve-a-port
PKG     := ./cmd/serve-a-port
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build run demo test cover lint tidy clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(PKG)

run:
	go run $(PKG)

demo:
	go run $(PKG) demo

test:
	go test ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

lint:
	golangci-lint run ./...

tidy:
	go mod tidy

clean:
	rm -rf bin dist coverage.out
