package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromFileAppliesEnvironmentOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sparkrc")
	config := []byte("endpoint: http://file.example\nmodel: file-model\nsystem_prompt: file prompt\nqueue_limit: 2\n")
	if err := os.WriteFile(path, config, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SPARK_ENDPOINT", "http://env.example")
	t.Setenv("SPARK_MODEL", "env-model")
	t.Setenv("SPARK_SYSTEM_PROMPT", "env prompt")
	t.Setenv("SPARK_API_KEY", "secret")
	t.Setenv("SPARK_QUEUE_LIMIT", "4")

	got, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if got.Endpoint != "http://env.example" || got.Model != "env-model" || got.SystemPrompt != "env prompt" || got.APIKey != "secret" || got.QueueLimit != 4 {
		t.Fatalf("unexpected config: %+v", got)
	}
}

func TestLoadFromFileUsesDefaultsWhenMissing(t *testing.T) {
	got, err := LoadFromFile(filepath.Join(t.TempDir(), ".sparkrc"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Endpoint != defaultEndpoint || got.QueueLimit != defaultQueueSize {
		t.Fatalf("unexpected defaults: %+v", got)
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
