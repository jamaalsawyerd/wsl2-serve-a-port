BINARY := serve-a-port
PKG    := ./cmd/serve-a-port

.PHONY: build run test cover lint tidy clean

build:
	go build -o bin/$(BINARY) $(PKG)

run:
	go run $(PKG)

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
