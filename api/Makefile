.PHONY: run build test tidy fmt vet

run:
	go run ./cmd/api

build:
	CGO_ENABLED=0 go build -o bin/arib-license-api ./cmd/api

test:
	go test ./...

tidy:
	go mod tidy

fmt:
	gofmt -w ./internal ./pkg ./cmd

vet:
	go vet ./...
