package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mikepjb/spark/internal/commands"
	"github.com/mikepjb/spark/internal/config"
	"github.com/mikepjb/spark/internal/files"
	modelconfig "github.com/mikepjb/spark/internal/model"
	"github.com/mikepjb/spark/internal/repl"
	"github.com/mikepjb/spark/internal/skills"
	"github.com/mikepjb/spark/internal/tools"
	"github.com/mikepjb/spark/internal/view"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Alas, there's been an error loading configuration: %v\n", err)
		os.Exit(1)
	}

	workspace, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Alas, there's been an error finding the workspace: %v\n", err)
		os.Exit(1)
	}

	modelManager, err := modelconfig.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Alas, there's been an error configuring models: %v\n", err)
		os.Exit(1)
	}
	currentModel := modelManager.Current()

	registry, err := tools.New(workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Alas, there's been an error configuring tools: %v\n", err)
		os.Exit(1)
	}

	filePaths, err := files.List(workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Alas, there's been an error indexing workspace files: %v\n", err)
		os.Exit(1)
	}

	skillRoots, err := skillRoots(workspace, cfg.SkillPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Alas, there's been an error finding skills: %v\n", err)
		os.Exit(1)
	}
	skillCatalog, err := skills.Discover(skillRoots)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Alas, there's been an error indexing skills: %v\n", err)
		os.Exit(1)
	}

	coordinator := repl.New(currentModel.Client, repl.Config{
		Model:          currentModel.Model,
		SystemPrompt:   cfg.SystemPrompt,
		QueueLimit:     cfg.QueueLimit,
		ContextLimit:   cfg.ContextLimit,
		ToolRoundLimit: cfg.ToolRoundLimit,
		Tools:          registry,
	})
	modelOptions := make([]commands.ModelOption, 0, len(modelManager.Choices()))
	for _, choice := range modelManager.Choices() {
		modelOptions = append(modelOptions, commands.ModelOption{Name: choice.Name, Provider: choice.Provider, Model: choice.Model})
	}
	commandEngine := commands.New(skillCatalog, modelOptions, filePaths, func(name string) (string, error) {
		choice, err := modelManager.Select(name)
		if err != nil {
			return "", err
		}
		if err := coordinator.SetModel(choice.Client, choice.Model); err != nil {
			return "", err
		}
		return choice.Model, nil
	})
	if err := view.Start(coordinator, currentModel.Model, cfg.ContextLimit, commandEngine, workspace); err != nil {
		fmt.Fprintf(os.Stderr, "Alas, there's been an error: %v\n", err)
		os.Exit(1)
	}
}

func skillRoots(workspace string, configured []string) ([]string, error) {
	if len(configured) > 0 {
		roots := make([]string, 0, len(configured))
		for _, path := range configured {
			if filepath.IsAbs(path) {
				roots = append(roots, path)
			} else {
				roots = append(roots, filepath.Join(workspace, path))
			}
		}
		return roots, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("find home directory: %w", err)
	}
	return []string{
		filepath.Join(workspace, ".agents", "skills"),
		filepath.Join(home, ".agents", "skills"),
	}, nil
}
