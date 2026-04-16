.PHONY: help build run docker-up docker-down clean test fmt lint

## help: display this help message
help:
	@echo "Available commands:"
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' |  sed -e 's/^/ /'

## build: build the bot binary
build:
	go build -o flower-bot .
	@echo "✅ Build successful: ./flower-bot"

## run: run the bot locally
run:
	go run main.go

## fmt: format code
fmt:
	go fmt ./...

## lint: run linter
lint:
	golangci-lint run ./...

## test: run tests
test:
	go test -v ./...

## deps: download dependencies
deps:
	go mod download
	go mod tidy

## docker-build: build docker image
docker-build:
	docker build -t flower-bot:latest .

## docker-up: start docker compose
docker-up:
	docker-compose up -d

## docker-down: stop docker compose
docker-down:
	docker-compose down

## docker-logs: show docker logs
docker-logs:
	docker-compose logs -f bot

## docker-clean: remove docker containers and volumes
docker-clean:
	docker-compose down -v

## clean: clean build artifacts
clean:
	rm -f flower-bot
	go clean

## db-init: initialize database
db-init:
	psql -U flower_user -h localhost -d flower_bot -f db/init.sql

## env-setup: copy .env.example to .env
env-setup:
	cp .env.example .env
	@echo "✅ .env file created. Please fill in your values."

## all: install deps, format, build
all: deps fmt build
	@echo "✅ All done!"
