package model

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mikepjb/spark/internal/config"
	"github.com/mikepjb/spark/internal/llm"
)

type Choice struct {
	Name     string
	Provider string
	Model    string
	Client   llm.Client
}

type Manager struct {
	choices map[string]Choice
	active  string
}

func New(cfg config.Config) (*Manager, error) {
	manager := &Manager{choices: make(map[string]Choice)}
	if len(cfg.Models) == 0 {
		name := cfg.Model
		if name == "" {
			name = "default"
		}
		client, err := newClient(cfg.Endpoint, cfg.APIKey)
		if err != nil {
			return nil, err
		}
		manager.choices[name] = Choice{Name: name, Provider: "default", Model: cfg.Model, Client: client}
		manager.active = name
		return manager, nil
	}

	for name, definition := range cfg.Models {
		provider, ok := cfg.Providers[definition.Provider]
		if !ok {
			return nil, fmt.Errorf("model %q references unknown provider %q", name, definition.Provider)
		}
		if strings.TrimSpace(definition.Model) == "" {
			return nil, fmt.Errorf("model %q has an empty model name", name)
		}
		apiKey := os.Getenv(providerKey(definition.Provider))
		client, err := newClient(provider.Endpoint, apiKey)
		if err != nil {
			return nil, fmt.Errorf("configure model %q: %w", name, err)
		}
		manager.choices[name] = Choice{Name: name, Provider: definition.Provider, Model: definition.Model, Client: client}
	}
	manager.active = cfg.Model
	if _, ok := manager.choices[manager.active]; !ok {
		if manager.active == "" {
			choices := manager.Choices()
			manager.active = choices[0].Name
		} else {
			return nil, fmt.Errorf("configured active model %q does not exist", manager.active)
		}
	}
	return manager, nil
}

func (m *Manager) Choices() []Choice {
	result := make([]Choice, 0, len(m.choices))
	for _, choice := range m.choices {
		result = append(result, choice)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (m *Manager) Current() Choice { return m.choices[m.active] }

func (m *Manager) Select(name string) (Choice, error) {
	choice, ok := m.choices[name]
	if !ok {
		return Choice{}, fmt.Errorf("model %q is not configured", name)
	}
	m.active = name
	return choice, nil
}

func newClient(endpoint, apiKey string) (llm.Client, error) {
	return llm.NewClient(llm.ClientConfig{Endpoint: endpoint, APIKey: apiKey})
}

func providerKey(provider string) string {
	var b strings.Builder
	b.WriteString("SPARK_")
	for _, r := range strings.ToUpper(provider) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	b.WriteString("_API_KEY")
	return b.String()
}
