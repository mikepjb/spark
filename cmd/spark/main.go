package main

import (
	"fmt"
	"os"

	"github.com/mikepjb/spark/internal/config"
	"github.com/mikepjb/spark/internal/llm"
	"github.com/mikepjb/spark/internal/repl"
	"github.com/mikepjb/spark/internal/view"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Alas, there's been an error loading configuration: %v\n", err)
		os.Exit(1)
	}

	client, err := llm.NewClient(llm.ClientConfig{
		Endpoint: cfg.Endpoint,
		APIKey:   cfg.APIKey,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Alas, there's been an error connecting to the LLM: %v\n", err)
		os.Exit(1)
	}

	coordinator := repl.New(client, repl.Config{
		Model:        cfg.Model,
		SystemPrompt: cfg.SystemPrompt,
		QueueLimit:   cfg.QueueLimit,
	})
	if err := view.Start(coordinator, cfg.Model); err != nil {
		fmt.Fprintf(os.Stderr, "Alas, there's been an error: %v\n", err)
		os.Exit(1)
	}
}
