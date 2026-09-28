.PHONY: dev install lint test config eval \
	eval-qwen35-0.8b eval-qwen35-2b eval-qwen35-4b eval-qwen35-9b \
	eval-minicpm5-2b eval-gemma4-e2b eval-gemma4-e4b eval-granite41-3b

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
	@test -n "$(EVAL_MODEL)" || { echo "Set EVAL_MODEL to a configured Spark profile" >&2; exit 1; }
	@mkdir -p "$(dir $(EVAL_SPARK))"
	go build -o "$(EVAL_SPARK)" ./cmd/spark
	go run ./cmd/spark-eval --suite "$(EVAL_SUITE)" $(if $(wildcard $(EVAL_MANIFEST)),--manifest "$(EVAL_MANIFEST)") \
		--model "$(EVAL_MODEL)" --runs "$(EVAL_RUNS)" --spark "$(EVAL_SPARK)" \
		--output "$(EVAL_OUTPUT)"

eval-qwen35-0.8b: EVAL_MODEL = qwen35-0.8b
eval-qwen35-2b: EVAL_MODEL = qwen35-2b
eval-qwen35-4b: EVAL_MODEL = qwen35-4b
eval-qwen35-9b: EVAL_MODEL = qwen35-9b
eval-minicpm5-2b: EVAL_MODEL = minicpm5-2b
eval-gemma4-e2b: EVAL_MODEL = gemma4-e2b
eval-gemma4-e4b: EVAL_MODEL = gemma4-e4b
eval-granite41-3b: EVAL_MODEL = granite41-3b

eval-qwen35-0.8b eval-qwen35-2b eval-qwen35-4b eval-qwen35-9b \
eval-minicpm5-2b eval-gemma4-e2b eval-gemma4-e4b eval-granite41-3b: eval

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
