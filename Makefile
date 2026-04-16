.PHONY: build test lint clean cover bench install

BIN      := dist/boldkit
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -ldflags "-X main.version=$(VERSION)"

build:
	@mkdir -p dist
	go build $(LDFLAGS) -o $(BIN) ./boldkit

install:
	go install $(LDFLAGS) ./boldkit

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run --timeout=10m ./...

bench:
	go test -bench=. -benchmem ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

clean:
	rm -f coverage.out coverage.html
	rm -rf dist/
