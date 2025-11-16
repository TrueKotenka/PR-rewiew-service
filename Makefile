.PHONY: build run test clean migrate

build:
	docker-compose build

run:
	docker-compose up

test:
	go test ./...

clean:
	docker-compose down -v
	rm -f server

migrate:
	docker-compose exec db psql -U user -d review_service -f /docker-entrypoint-initdb.d/001_init.sql

dev:
	go run cmd/server/main.go

lint:
	golangci-lint run

# Database operations
db-shell:
	docker-compose exec db psql -U user -d review_service

# Build without docker for local development
build-local:
	CGO_ENABLED=0 go build -o server ./cmd/server