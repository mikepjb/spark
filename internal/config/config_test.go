package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromFileAppliesEnvironmentOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sparkrc")
	config := []byte("endpoint: http://file.example\nmodel: file-model\nsystem_prompt: file prompt\nqueue_limit: 2\ncontext_limit: 4096\n")
	if err := os.WriteFile(path, config, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SPARK_ENDPOINT", "http://env.example")
	t.Setenv("SPARK_MODEL", "env-model")
	t.Setenv("SPARK_SYSTEM_PROMPT", "env prompt")
	t.Setenv("SPARK_API_KEY", "secret")
	t.Setenv("SPARK_QUEUE_LIMIT", "4")
	t.Setenv("SPARK_CONTEXT_LIMIT", "8192")

	got, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if got.Endpoint != "http://env.example" || got.Model != "env-model" || got.SystemPrompt != "env prompt" || got.APIKey != "secret" || got.QueueLimit != 4 || got.ContextLimit != 8192 {
		t.Fatalf("unexpected config: %+v", got)
	}
}

func TestLoadFromFileUsesDefaultsWhenMissing(t *testing.T) {
	got, err := LoadFromFile(filepath.Join(t.TempDir(), ".sparkrc"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Endpoint != defaultEndpoint || got.SystemPrompt != defaultSystemPrompt || got.QueueLimit != defaultQueueSize || got.ContextLimit != defaultContextLimit {
		t.Fatalf("unexpected defaults: %+v", got)
	}
}

func TestLoadFromFileUsesDefaultSystemPromptForEmptyConfigValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sparkrc")
	if err := os.WriteFile(path, []byte("system_prompt: \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.SystemPrompt != defaultSystemPrompt {
		t.Fatalf("unexpected system prompt: %q", got.SystemPrompt)
	}
}

func TestLoadFromFileRejectsInvalidContextLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sparkrc")
	if err := os.WriteFile(path, []byte("context_limit: 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFromFile(path); err == nil {
		t.Fatal("expected invalid context limit error")
	}
}

func TestLoadFromFileRejectsInvalidQueueLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sparkrc")
	if err := os.WriteFile(path, []byte("queue_limit: 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFromFile(path); err == nil {
		t.Fatal("expected invalid queue limit error")
	}
}

func TestLoadFromFileReadsModelsProvidersAndSkillPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sparkrc")
	contents := []byte("model: local\nproviders:\n  local:\n    endpoint: http://127.0.0.1:7777\nmodels:\n  local:\n    provider: local\n    model: qwen\nskill_paths:\n  - .agents/skills\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "local" || got.Providers["local"].Endpoint != "http://127.0.0.1:7777" || got.Models["local"].Model != "qwen" || len(got.SkillPaths) != 1 {
		t.Fatalf("config = %+v", got)
	}
}
