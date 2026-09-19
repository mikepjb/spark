package model

import (
	"testing"

	"github.com/mikepjb/spark/internal/config"
)

func TestNewSupportsNamedModelsAndProviderKeys(t *testing.T) {
	t.Setenv("SPARK_OPENAI_API_KEY", "secret")
	manager, err := New(config.Config{
		Model: "cloud",
		Providers: map[string]config.ProviderConfig{
			"openai": {Endpoint: "https://example.test"},
		},
		Models: map[string]config.ModelConfig{
			"cloud": {Provider: "openai", Model: "gpt-test"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := manager.Current(); got.Name != "cloud" || got.Model != "gpt-test" || got.Provider != "openai" || got.Client == nil {
		t.Fatalf("current choice = %+v", got)
	}
}

func TestNewSupportsLegacyModel(t *testing.T) {
	manager, err := New(config.Config{Endpoint: "https://example.test", Model: "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	if got := manager.Current(); got.Name != "legacy" || got.Model != "legacy" || got.Provider != "default" {
		t.Fatalf("current choice = %+v", got)
	}
}
