package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mikepjb/spark/internal/commands"
	"github.com/mikepjb/spark/internal/config"
	"github.com/mikepjb/spark/internal/files"
	"github.com/mikepjb/spark/internal/llm"
	modelconfig "github.com/mikepjb/spark/internal/model"
	"github.com/mikepjb/spark/internal/repl"
	"github.com/mikepjb/spark/internal/skills"
	"github.com/mikepjb/spark/internal/tools"
	"github.com/mikepjb/spark/internal/view"
)

const (
	exitOK               = 0
	exitRuntime          = 1
	exitUsage            = 2
	exitInterrupted      = 130
	promptEditorSentinel = "\x00spark-open-editor\x00"
)

var errProfileSelection = errors.New("invalid model profile")

type cliOptions struct {
	Prompt       string
	PromptSet    bool
	PromptEditor bool
	Skill        string
	Model        string
	Format       string
	Headless     bool
}

type application struct {
	coordinator  *repl.Coordinator
	modelName    string
	profileName  string
	contextSize  int
	commands     *commands.Engine
	workspace    string
	skillCatalog *skills.Catalog
}

type resultDocument struct {
	SchemaVersion   int                `json:"schema_version"`
	SelectedProfile string             `json:"selected_profile"`
	ResolvedModel   string             `json:"resolved_model"`
	Answer          string             `json:"answer"`
	DurationMS      int64              `json:"duration_ms"`
	Rounds          int                `json:"rounds"`
	Usage           llm.Usage          `json:"usage"`
	ToolCalls       []repl.RunToolCall `json:"tool_calls"`
	Status          string             `json:"status"`
	Error           string             `json:"error"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	options, err := parseArgs(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		fmt.Fprintf(stderr, "spark: %v\n", err)
		return exitUsage
	}
	if options.Headless && options.Format == "" {
		options.Format = "text"
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "spark: configuration: %v\n", err)
		return exitRuntime
	}
	workspace, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "spark: workspace: %v\n", err)
		return exitRuntime
	}
	app, err := bootstrap(cfg, workspace, options.Model)
	if err != nil {
		fmt.Fprintf(stderr, "spark: startup: %v\n", err)
		if errors.Is(err, errProfileSelection) {
			return exitUsage
		}
		return exitRuntime
	}
	if options.Headless {
		defer app.coordinator.Close()
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		return runHeadless(ctx, app, options, stdin, stdout, stderr)
	}
	defer app.coordinator.Close()
	if err := view.Start(app.coordinator, app.modelName, app.contextSize, app.commands, app.workspace); err != nil {
		fmt.Fprintf(stderr, "spark: %v\n", err)
		return exitRuntime
	}
	return exitOK
}

func parseArgs(args []string, stderr io.Writer) (cliOptions, error) {
	var options cliOptions
	args = markBarePromptFlags(args)
	flags := flag.NewFlagSet("spark", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&options.Prompt, "p", "", "submit one prompt and exit; without a value, open $EDITOR")
	flags.StringVar(&options.Prompt, "prompt", "", "submit one prompt and exit; without a value, open $EDITOR")
	flags.StringVar(&options.Skill, "s", "", "activate one discovered skill")
	flags.StringVar(&options.Skill, "skill", "", "activate one discovered skill")
	flags.StringVar(&options.Model, "m", "", "select a configured model profile")
	flags.StringVar(&options.Model, "model", "", "select a configured model profile")
	flags.StringVar(&options.Format, "format", "", "headless output format: text or json")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: spark [-p [PROMPT]] [-s SKILL] [-m PROFILE] [--format text|json]")
		fmt.Fprintln(stderr, "With no prompt or skill, Spark starts the interactive TUI.")
		fmt.Fprintln(stderr, "Use -p without PROMPT to write a prompt in $EDITOR.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	var skillSet, modelSet bool
	flags.Visit(func(item *flag.Flag) {
		if item.Name == "p" || item.Name == "prompt" {
			options.PromptSet = true
		}
		if item.Name == "s" || item.Name == "skill" {
			skillSet = true
		}
		if item.Name == "m" || item.Name == "model" {
			modelSet = true
		}
	})
	if options.Prompt == promptEditorSentinel {
		options.Prompt = ""
		options.PromptEditor = true
	}
	if flags.NArg() != 0 {
		return options, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	options.Headless = options.Prompt != "" || options.PromptEditor || options.Skill != ""
	if options.Format != "" && options.Format != "text" && options.Format != "json" {
		return options, fmt.Errorf("--format must be text or json")
	}
	if options.Format != "" && !options.Headless {
		return options, fmt.Errorf("--format requires --prompt or --skill")
	}
	if options.PromptSet && options.Prompt == "" && !options.PromptEditor {
		return options, fmt.Errorf("--prompt must not be empty")
	}
	if skillSet && options.Skill == "" {
		return options, fmt.Errorf("--skill must not be empty")
	}
	if modelSet && options.Model == "" {
		return options, fmt.Errorf("--model must not be empty")
	}
	return options, nil
}

func markBarePromptFlags(args []string) []string {
	marked := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "-p" || arg == "--prompt" {
			if index+1 == len(args) || strings.HasPrefix(args[index+1], "-") {
				marked = append(marked, arg+"="+promptEditorSentinel)
				continue
			}
		}
		marked = append(marked, arg)
	}
	return marked
}

func bootstrap(cfg config.Config, workspace, selectedProfile string) (*application, error) {
	modelManager, err := modelconfig.New(cfg)
	if err != nil {
		return nil, err
	}
	if selectedProfile != "" {
		if _, err := modelManager.Select(selectedProfile); err != nil {
			return nil, fmt.Errorf("%w: %w", errProfileSelection, err)
		}
	}
	currentModel := modelManager.Current()
	registry, err := tools.New(workspace)
	if err != nil {
		return nil, fmt.Errorf("configure tools: %w", err)
	}
	var filePaths []string
	if !isHomeDirectory(workspace) {
		filePaths, err = files.List(workspace)
		if err != nil {
			return nil, fmt.Errorf("index workspace files: %w", err)
		}
	}
	skillRootPaths, err := skillRoots(workspace, cfg.SkillPaths)
	if err != nil {
		return nil, err
	}
	skillCatalog, err := skills.Discover(skillRootPaths)
	if err != nil {
		return nil, fmt.Errorf("index skills: %w", err)
	}
	agentPrompt, err := userAgentPrompt()
	if err != nil {
		return nil, err
	}
	coordinator := repl.New(currentModel.Client, repl.Config{
		Model: currentModel.Model, SystemPrompt: cfg.SystemPrompt, AgentPrompt: agentPrompt,
		Environment: repl.Environment{WorkingDirectory: workspace, IsGitRepository: isGitRepository(workspace), Platform: runtime.GOOS},
		QueueLimit:  cfg.QueueLimit, ContextLimit: cfg.ContextLimit, ToolCallLimit: cfg.ToolCallLimit,
		ShowReasoning: cfg.ShowReasoning, Tools: registry,
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
	return &application{coordinator: coordinator, modelName: currentModel.Model, profileName: currentModel.Name, contextSize: cfg.ContextLimit, commands: commandEngine, workspace: workspace, skillCatalog: skillCatalog}, nil
}

func runHeadless(ctx context.Context, app *application, options cliOptions, stdin io.Reader, stdout, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	prompt := options.Prompt
	if options.PromptEditor {
		editedPrompt, err := editPrompt(ctx, stdin, stderr)
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return exitInterrupted
			}
			fmt.Fprintf(stderr, "spark: editor: %v\n", err)
			if errors.Is(err, errEmptyPrompt) {
				return exitUsage
			}
			return exitRuntime
		}
		prompt = editedPrompt
	}
	var skillUses []repl.SkillUse
	if options.Skill != "" {
		_, body, err := app.skillCatalog.Load(options.Skill)
		if err != nil {
			fmt.Fprintf(stderr, "spark: skill: %v\n", err)
			return exitUsage
		}
		skillUses = append(skillUses, repl.SkillUse{Name: options.Skill, Body: body})
		if prompt == "" {
			prompt = "Apply the selected skill to this workspace. Follow its instructions and report the result."
		}
	}
	requestID, err := app.coordinator.SubmitSubmission(repl.Submission{Display: prompt, Prompt: prompt, Skills: skillUses})
	if err != nil {
		fmt.Fprintf(stderr, "spark: request: %v\n", err)
		return exitRuntime
	}
	var result *repl.RunResult
	for {
		select {
		case <-ctx.Done():
			app.coordinator.CancelRequest(requestID)
			ctx = context.Background()
		case event, ok := <-app.coordinator.Events():
			if !ok {
				fmt.Fprintln(stderr, "spark: coordinator stopped before returning a result")
				return exitRuntime
			}
			switch event.Kind {
			case repl.EventChunk:
				if options.Format != "json" {
					if _, err := io.WriteString(stdout, event.Content); err != nil {
						fmt.Fprintf(stderr, "spark: write answer: %v\n", err)
						app.coordinator.Cancel()
						return exitRuntime
					}
				}
			case repl.EventToolStarted:
				fmt.Fprintf(stderr, "tool: %s\n", event.Content)
			case repl.EventToolCompleted:
				state := "ok"
				if event.Failed {
					state = "failed"
				}
				fmt.Fprintf(stderr, "tool %s: %s (%s)\n", event.ToolCallID, event.Content, state)
			case repl.EventCompleted, repl.EventFailed, repl.EventCancelled:
				result = event.Run
				if result == nil {
					fmt.Fprintln(stderr, "spark: request ended without structured result")
					return exitRuntime
				}
				if options.Format == "json" {
					if err := writeJSONResult(stdout, app, result); err != nil {
						fmt.Fprintf(stderr, "spark: write result: %v\n", err)
						return exitRuntime
					}
				} else if result.Status != "succeeded" && result.Answer == "" {
					fmt.Fprintln(stderr)
				}
				if result.Error != "" {
					fmt.Fprintf(stderr, "spark: %s\n", result.Error)
				}
				switch result.Status {
				case "succeeded":
					if options.Format != "json" {
						fmt.Fprintln(stdout)
					}
					return exitOK
				case "cancelled":
					return exitInterrupted
				default:
					return exitRuntime
				}
			}
		}
	}
}

var errEmptyPrompt = errors.New("prompt file is empty")

func editPrompt(ctx context.Context, stdin io.Reader, stderr io.Writer) (string, error) {
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		return "", fmt.Errorf("EDITOR is not set")
	}
	promptFile, err := os.CreateTemp("", "spark-prompt-*.md")
	if err != nil {
		return "", fmt.Errorf("create temporary prompt file: %w", err)
	}
	path := promptFile.Name()
	defer func() { _ = os.Remove(path) }()
	if err := promptFile.Close(); err != nil {
		return "", fmt.Errorf("close temporary prompt file: %w", err)
	}
	command := exec.CommandContext(ctx, editor, path)
	command.Stdin = stdin
	command.Stdout = stderr
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("run %q: %w", editor, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read temporary prompt file: %w", err)
	}
	prompt := strings.TrimSpace(string(data))
	if prompt == "" {
		return "", errEmptyPrompt
	}
	return prompt, nil
}

func writeJSONResult(writer io.Writer, app *application, run *repl.RunResult) error {
	toolCalls := run.ToolCalls
	if toolCalls == nil {
		toolCalls = []repl.RunToolCall{}
	}
	return json.NewEncoder(writer).Encode(resultDocument{
		SchemaVersion: 1, SelectedProfile: app.profileName, ResolvedModel: run.Model,
		Answer: run.Answer, DurationMS: run.Duration.Milliseconds(), Usage: run.Usage,
		Rounds:    run.Rounds,
		ToolCalls: toolCalls, Status: run.Status, Error: run.Error,
	})
}

func userAgentPrompt() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return userAgentPromptFromHome(home)
}

func userAgentPromptFromHome(home string) (string, error) {
	path := filepath.Join(home, ".agents", "AGENTS.md")
	data, err := os.ReadFile(path)
	if err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return "", nil
}

func isHomeDirectory(workspace string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	return sameDirectory(workspace, home)
}

func sameDirectory(first, second string) bool {
	firstInfo, err := os.Stat(first)
	if err != nil {
		return false
	}
	secondInfo, err := os.Stat(second)
	if err != nil {
		return false
	}
	return os.SameFile(firstInfo, secondInfo)
}

func isGitRepository(root string) bool {
	output, err := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil && strings.TrimSpace(string(output)) == "true"
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
	return []string{filepath.Join(workspace, ".agents", "skills"), filepath.Join(home, ".agents", "skills")}, nil
}
