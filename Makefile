.PHONY: dev

dev:
	go run ./cmd/main.go

test:
	go test -v ./...
