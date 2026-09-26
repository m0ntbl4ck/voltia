# Local settings live in .env, which is not versioned; see .env.example.
-include .env
export

.PHONY: up down db db-down sqlc dev-api build test lint

up:
	docker compose up -d --build --wait

down:
	docker compose down

db:
	docker compose up -d --wait postgres

db-down:
	docker compose down

sqlc:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate

dev-api:
	go run ./cmd/voltia

build:
	go build -o bin/voltia ./cmd/voltia

test:
	go test ./...

lint:
	go vet ./...
