package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

const maxDocOutput = 16 * 1024

var docEntry = regexp.MustCompile(`^[a-zA-Z0-9_@][a-zA-Z0-9_@./:$-]*$`)

// doc exposes selected read-only operations from the user's offline doc script.
// Python and Node member inspection import packages, so those combinations
// are deliberately excluded from Spark's tool surface.
func (r *Registry) doc(ctx context.Context, args map[string]json.RawMessage) Result {
	if r.docPath == "" {
		return errorResult(fmt.Errorf("doc executable is unavailable"))
	}
	if err := validateKeys(args, "action", "language", "entry", "symbol", "pattern", "scope"); err != nil {
		return errorResult(err)
	}
	action, err := stringArgument(args, "action", true)
	if err != nil {
		return errorResult(err)
	}
	language, err := stringArgument(args, "language", true)
	if err != nil {
		return errorResult(err)
	}
	if language != "go" && language != "java" && language != "node" && language != "ts" && language != "python" {
		return errorResult(fmt.Errorf("unsupported documentation language %q", language))
	}
	entry, err := stringArgument(args, "entry", false)
	if err != nil {
		return errorResult(err)
	}
	symbol, err := stringArgument(args, "symbol", false)
	if err != nil {
		return errorResult(err)
	}
	pattern, err := stringArgument(args, "pattern", false)
	if err != nil {
		return errorResult(err)
	}
	scope, err := stringArgument(args, "scope", false)
	if err != nil {
		return errorResult(err)
	}
	for name, value := range map[string]string{"entry": entry, "symbol": symbol} {
		if value != "" && (len(value) > 256 || !docEntry.MatchString(value) || strings.Contains(value, "..")) {
			return errorResult(fmt.Errorf("invalid %s for Doc", name))
		}
	}
	if len(pattern) > 256 {
		return errorResult(fmt.Errorf("Doc pattern is too long"))
	}
	var commandArgs []string
	switch action {
	case "list":
		if entry != "" || symbol != "" || pattern != "" {
			return errorResult(fmt.Errorf("Doc list accepts only language and scope"))
		}
		if scope == "" {
			scope = "std"
		}
		// Project/module listing may download dependencies or inspect package code.
		if scope != "std" {
			return errorResult(fmt.Errorf("Doc list supports only std scope"))
		}
		commandArgs = []string{"list", language, scope}
	case "members":
		if entry == "" || symbol != "" || pattern != "" || scope != "" {
			return errorResult(fmt.Errorf("Doc members requires entry and no other options"))
		}
		if language != "go" && language != "java" && language != "ts" {
			return errorResult(fmt.Errorf("Doc members is unavailable for %s because it can import package code", language))
		}
		commandArgs = []string{"members", language, entry}
	case "show":
		if entry == "" || pattern != "" || scope != "" {
			return errorResult(fmt.Errorf("Doc show requires entry and optional symbol"))
		}
		if language == "python" {
			return errorResult(fmt.Errorf("Doc show is unavailable for python because it can import package code"))
		}
		if language == "node" && symbol != "" {
			return errorResult(fmt.Errorf("Doc show does not support a symbol for node"))
		}
		commandArgs = []string{"show", language, entry}
		if symbol != "" {
			commandArgs = append(commandArgs, symbol)
		}
	case "search":
		if pattern == "" || entry != "" || symbol != "" {
			return errorResult(fmt.Errorf("Doc search requires pattern and optional scope"))
		}
		if scope == "" {
			scope = "std"
		}
		validScope := scope == "std" || scope == "project" || (scope == "cache" && (language == "go" || language == "java"))
		if language == "ts" {
			validScope = scope == "project"
		}
		if language == "java" {
			validScope = scope == "std" || scope == "cache"
		}
		if !validScope {
			return errorResult(fmt.Errorf("unsupported Doc search scope %q for %s", scope, language))
		}
		commandArgs = []string{"search", language, pattern, scope}
	default:
		return errorResult(fmt.Errorf("unsupported Doc action %q", action))
	}

	command := exec.CommandContext(ctx, r.docPath, commandArgs...)
	command.Dir = r.root
	// Go package lookup must stay offline even when a module is absent locally.
	command.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off")
	var stdout, stderr boundedBuffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err = command.Run()
	if ctx.Err() != nil {
		return errorResult(fmt.Errorf("Doc %s timed out or was cancelled: %w", action, ctx.Err()))
	}
	output := stdout.String()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && action == "search" && exit.ExitCode() == 1 && stderr.Len() == 0 {
			return Result{Summary: fmt.Sprintf("Doc search %s", language), Content: "[no documentation found]"}
		}
		if stderr.Len() > 0 {
			output = stderr.String()
		}
		if output == "" {
			output = err.Error()
		}
		return Result{Summary: "Doc failed", Content: formatDocOutput(output), Failed: true}
	}
	content := formatDocOutput(output)
	if content == "" {
		content = "[no documentation found]"
	}
	return Result{Summary: fmt.Sprintf("Doc %s %s", action, language), Content: content}
}

func formatDocOutput(output string) string {
	truncated := len(output) > maxDocOutput
	if truncated {
		output = output[:maxDocOutput]
	}
	content := formatOutput(strings.Split(strings.TrimRight(output, "\n"), "\n"))
	if truncated {
		content += "\n\n[documentation output truncated]"
	}
	return content
}
