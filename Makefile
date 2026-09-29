.PHONY: dev install lint test config eval

EVAL_SUITE ?= evaluation/suites/practical.yaml
EVAL_MANIFEST ?= evaluation/manifest.yaml
EVAL_RUNS ?= 3
EVAL_OUTPUT ?= evaluation/results/runs.jsonl
EVAL_SPARK ?= $(CURDIR)/evaluation/results/spark

dev:
	go run ./cmd/spark/main.go

install:
	go install ./cmd/spark

lint:
	go tool golangci-lint run --tests=false

test: lint
	go test -v ./...

eval:
	@mkdir -p "$(dir $(EVAL_SPARK))"
	go build -o "$(EVAL_SPARK)" ./cmd/spark
	go run ./cmd/spark-eval --suite "$(EVAL_SUITE)" $(if $(wildcard $(EVAL_MANIFEST)),--manifest "$(EVAL_MANIFEST)") \
		$(if $(EVAL_MODEL),--model "$(EVAL_MODEL)") --runs "$(EVAL_RUNS)" --spark "$(EVAL_SPARK)" \
		--output "$(EVAL_OUTPUT)"

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
		'queue_limit: 5' \
		'context_limit: 64000' \
		'tool_call_limit: 8' \
		'show_reasoning: true' \
		> "$$config_file" \
	) || { \
		echo "Could not create Spark config: $$config_file" >&2; \
		exit 1; \
	}; \
	echo "Created Spark config: $$config_file"
