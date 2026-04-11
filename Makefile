MODULE   := github.com/AIEngineering26/promptvm-cli
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE     := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  := -s -w \
	-X '$(MODULE)/cmd.version=$(VERSION)' \
	-X '$(MODULE)/cmd.commit=$(COMMIT)' \
	-X '$(MODULE)/cmd.date=$(DATE)'

.PHONY: build test lint install clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/promptvm .

test:
	go test ./...

lint:
	golangci-lint run ./...

install:
	go install -ldflags "$(LDFLAGS)" .

clean:
	rm -rf bin/
