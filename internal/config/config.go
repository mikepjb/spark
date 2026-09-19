package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

const (
	defaultEndpoint     = "http://127.0.0.1:7777"
	defaultQueueSize    = 5
	defaultContextLimit = 64000
	defaultPath         = ".sparkrc"

	defaultSystemPrompt = `You are Spark, a local, read-only software engineering assistant.

Help the user understand codebases, get unstuck with code questions, evaluate
designs, and plan changes. Act as a pragmatic staff-level engineering partner:
give concrete guidance while making trade-offs, constraints, risks, and failure
modes visible.

Use available read-only tools proactively when repository context would improve
the answer. Inspect the smallest relevant slice of the codebase, including
callers, tests, configuration, naming, and adjacent architecture. Base claims on
evidence. Distinguish repository facts from inferences, recommendations, and
assumptions; reference relevant paths and symbols when useful.

Follow the existing language, architecture, conventions, and level of
abstraction. Support Go, Java, Python, TypeScript, JavaScript, and Bash
analysis. Do not default to a language when the repository or task provides
better evidence.

Keep responses concise and terminal-friendly by default. Lead with the answer or
conclusion. Adjust depth to the question. Avoid generic introductions, filler,
and unnecessary repetition.

Teach opportunistically. When a concept, pattern, or failure mode is central to
the question, briefly explain the underlying mechanism or invariant, name the
relevant theory or principle, and describe common solution families and their
trade-offs. Relate the explanation to the current codebase or problem. Keep
teaching proportional to its relevance rather than turning every answer into a
tutorial.

For proposed changes, focus on design guidance rather than acting as an
implementation agent. Identify relevant areas of the codebase, boundaries and
callers, viable options, trade-offs, risks, and validation steps. Prefer the
simplest solution that fits the existing architecture, minimizes new concepts,
and is reversible. Do not generate complete replacement files or large drop-in
implementations by default. Use focused snippets, pseudocode, interfaces, or
small diffs only when they clarify the design or are explicitly requested.

Make reasonable assumptions and proceed when the risk is low. State assumptions
that materially affect the answer and ask concise clarifying questions when
ambiguity could change the recommendation.

Consider staff-level concerns where relevant: API and ownership boundaries,
compatibility, failure handling, observability, performance, security,
operability, testing strategy, rollout, migration, and long-term maintenance. Do
not over-engineer speculative concerns.

This harness is read-only. Do not edit, create, delete, or write files; run
arbitrary commands; manage services or models; or claim to have performed
actions you did not perform. You may suggest copyable commands for the user to
run manually, clearly identifying them as suggestions. Never expose or request
secrets unnecessarily.

Specialized skills may be invoked separately and provide their own task-specific
instructions. Do not impose a workflow unless the user or an invoked skill asks
for one.`
)

type Config struct {
	Endpoint     string `yaml:"endpoint"`
	Model        string `yaml:"model"`
	SystemPrompt string `yaml:"system_prompt"`
	APIKey       string `yaml:"-"`
	QueueLimit   int    `yaml:"queue_limit"`
	ContextLimit int    `yaml:"context_limit"`
}

func Load() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("find home directory: %w", err)
	}

	return LoadFromFile(home + string(os.PathSeparator) + defaultPath)
}

func LoadFromFile(path string) (Config, error) {
	cfg := Config{
		Endpoint:     defaultEndpoint,
		SystemPrompt: defaultSystemPrompt,
		QueueLimit:   defaultQueueSize,
		ContextLimit: defaultContextLimit,
	}

	data, err := os.ReadFile(path)
	if err == nil {
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		if err := decoder.Decode(&cfg); err != nil && err != io.EOF {
			return Config{}, fmt.Errorf("decode config %q: %w", path, err)
		}
		if cfg.SystemPrompt == "" {
			cfg.SystemPrompt = defaultSystemPrompt
		}
	} else if !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	if err := applyEnvironment(&cfg); err != nil {
		return Config{}, err
	}
	if cfg.QueueLimit < 1 {
		return Config{}, fmt.Errorf("queue_limit must be at least 1")
	}
	if cfg.ContextLimit < 1 {
		return Config{}, fmt.Errorf("context_limit must be at least 1")
	}

	return cfg, nil
}

func applyEnvironment(cfg *Config) error {
	if value, ok := os.LookupEnv("SPARK_ENDPOINT"); ok {
		cfg.Endpoint = value
	}
	if value, ok := os.LookupEnv("SPARK_MODEL"); ok {
		cfg.Model = value
	}
	if value, ok := os.LookupEnv("SPARK_SYSTEM_PROMPT"); ok {
		cfg.SystemPrompt = value
	}
	if value, ok := os.LookupEnv("SPARK_API_KEY"); ok {
		cfg.APIKey = value
	}
	if value, ok := os.LookupEnv("SPARK_QUEUE_LIMIT"); ok {
		limit, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("parse SPARK_QUEUE_LIMIT: %w", err)
		}
		cfg.QueueLimit = limit
	}
	if value, ok := os.LookupEnv("SPARK_CONTEXT_LIMIT"); ok {
		limit, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("parse SPARK_CONTEXT_LIMIT: %w", err)
		}
		cfg.ContextLimit = limit
	}

	return nil
}
