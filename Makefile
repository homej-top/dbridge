.PHONY: build run test clean docker

build:
	go build -o db-sync-web-server ./cmd/server/

run: build
	./db-sync-web-server

test:
	go test -v ./...

clean:
	rm -f db-sync-web-server
	go clean

lint:
	golangci-lint run

docker:
	docker build -t dbridge:latest .

migrate:
	go run ./cmd/migrate/

wire:
	wire ./internal/...
