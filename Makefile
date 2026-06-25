.PHONY: gen mock fmt build run test lint up down

gen:
	protoc --proto_path=. --twirp_out=. --go_out=. rpc/*.proto

mock:
	go tool mockgen -destination=internal/chat/mock_assistant_test.go -package=chat github.com/acai-travel/tech-challenge/internal/chat Assistant
	go tool mockgen -destination=internal/chat/assistant/mock_completions_test.go -package=assistant github.com/acai-travel/tech-challenge/internal/chat/assistant completionsAPI

fmt:
	gofmt -w .

build:
	go build ./...

run:
	go run ./cmd/server

test:
	go test ./...

lint:
	go vet ./...

up:
	docker compose up -d

down:
	docker compose down
