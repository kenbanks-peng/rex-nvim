PREFIX ?= $(HOME)/.local

.PHONY: build test install
build:
	go build -o build/rex-nvim ./cmd/rex-nvim
test:
	go test -race ./...
install:
	mkdir -p "$(PREFIX)/bin"
	go build -o "$(PREFIX)/bin/rex-nvim" ./cmd/rex-nvim
