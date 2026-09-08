.PHONY: run worker migrate build test test-race test-integration fmt vet lint vuln sqlc openapi-check migrate-up migrate-down migrate-status migrate-redo migrate-create docker-up docker-down docker-logs tidy verify

run:
	go run ./cmd/api

worker:
	go run ./cmd/worker

migrate:
	go run ./cmd/migrate -command up

build:
	go build ./...

test:
	go test ./...

test-race:
	go test -race ./...

test-integration:
	go test -tags=integration ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

lint:
	staticcheck ./...

vuln:
	govulncheck ./...

sqlc:
	go tool sqlc generate

openapi-check:
	redocly lint api/openapi.yaml

migrate-up:
	go run ./cmd/migrate -command up

migrate-down:
	go run ./cmd/migrate -command down

migrate-status:
	go run ./cmd/migrate -command status

migrate-redo:
	go run ./cmd/migrate -command redo

migrate-create:
	go tool goose -dir database/migrations create $(name) sql

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f api worker

tidy:
	go mod tidy

verify: fmt sqlc test build vet