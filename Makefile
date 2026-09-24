.PHONY: db db-down dev-api dev-web build test lint

db:
	docker compose up -d --wait postgres

db-down:
	docker compose down

dev-api:
	go run ./cmd/voltia

dev-web:
	npm --prefix web run dev

build:
	npm --prefix web run build
	go build -o bin/voltia ./cmd/voltia

test:
	go test ./...

lint:
	go vet ./...
	npm --prefix web run lint
