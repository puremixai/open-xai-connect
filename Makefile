.PHONY: test lint build run

test:
	go test ./...

lint:
	go vet ./...

build:
	go build ./cmd/connect

run:
	go run ./cmd/connect
