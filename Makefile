.PHONY: dev install lint test config

dev:
	go run ./cmd/spark/main.go

install:
	go install ./cmd/spark

lint:
	golangci-lint run

test: lint
	go test -v ./...

config:
	@if [ -z "$(HOME)" ]; then \
		echo "HOME must be set to generate Spark config" >&2; \
		exit 1; \
	fi; \
	config_file="$(HOME)/.sparkrc"; \
	if [ -e "$$config_file" ] || [ -L "$$config_file" ]; then \
		echo "Spark config already exists: $$config_file"; \
		exit 0; \
	fi; \
	umask 077; \
	( set -C; printf '%s\n' \
		'endpoint: http://127.0.0.1:8080' \
		'model: ""' \
		'system_prompt: ""' \
		'queue_limit: 5' \
		'context_limit: 64000' \
		> "$$config_file" \
	) || { \
		echo "Could not create Spark config: $$config_file" >&2; \
		exit 1; \
	}; \
	echo "Created Spark config: $$config_file"
