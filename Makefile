.PHONY: run build tidy

run:
	go run ./cmd/server

build:
	go build -o bin/server ./cmd/server

tidy:
	go mod tidy
