package tools

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mikepjb/spark/internal/llm"
)

//go:embed definitions.json
var definitionsFS embed.FS

const (
	defaultLineLimit = 500
	maxLineLimit     = 500
	maxLineWidth     = 2000
	defaultLogCount  = 20
	maxLogCount      = 100
	commandTimeout   = 5 * time.Second
)

type Result struct {
	Summary string
	Content string
}

type Registry struct {
	root string
}

func New(root string) (*Registry, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root symlinks: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("stat workspace root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace root is not a directory: %s", resolved)
	}
	return &Registry{root: resolved}, nil
}

func (r *Registry) Definitions() []llm.ToolDefinition {
	data, err := definitionsFS.ReadFile("definitions.json")
	if err != nil {
		panic(fmt.Sprintf("read embedded tool definitions: %v", err))
	}
	var definitions []llm.ToolDefinition
	if err := json.Unmarshal(data, &definitions); err != nil {
		panic(fmt.Sprintf("decode embedded tool definitions: %v", err))
	}
	return definitions
}

func (r *Registry) Execute(ctx context.Context, call llm.ToolCall) Result {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	args, err := decodeArguments(call.Arguments)
	if err != nil {
		return errorResult(err)
	}

	switch call.Name {
	case "Read":
		return r.read(ctx, args)
	case "Glob":
		return r.glob(ctx, args)
	case "Grep":
		return r.grep(ctx, args)
	case "GitStatus":
		return r.git(ctx, args, "status", "--short", "--branch")
	case "GitDiff":
		return r.gitDiff(ctx, args)
	case "GitLog":
		return r.gitLog(ctx, args)
	case "GitShow":
		return r.gitShow(ctx, args)
	default:
		return errorResult(fmt.Errorf("unknown tool %q", call.Name))
	}
}

func decodeArguments(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]json.RawMessage{}, nil
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid tool arguments: %w", err)
	}
	return args, nil
}

func stringArgument(args map[string]json.RawMessage, name string, required bool) (string, error) {
	raw, ok := args[name]
	if !ok {
		if required {
			return "", fmt.Errorf("%s is required", name)
		}
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s must be a non-empty string", name)
	}
	return value, nil
}

func boolArgument(args map[string]json.RawMessage, name string) (bool, error) {
	raw, ok := args[name]
	if !ok {
		return false, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return value, nil
}

func intArgument(args map[string]json.RawMessage, name string, fallback, maximum int) (int, error) {
	raw, ok := args[name]
	if !ok {
		return fallback, nil
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil || value < 1 || value > maximum {
		return 0, fmt.Errorf("%s must be between 1 and %d", name, maximum)
	}
	return value, nil
}

func (r *Registry) read(ctx context.Context, args map[string]json.RawMessage) Result {
	if err := validateKeys(args, "filePath", "offset", "limit"); err != nil {
		return errorResult(err)
	}
	path, err := stringArgument(args, "filePath", true)
	if err != nil {
		return errorResult(err)
	}
	offset, err := intArgument(args, "offset", 1, int(^uint(0)>>1))
	if err != nil {
		return errorResult(err)
	}
	limit, err := intArgument(args, "limit", defaultLineLimit, maxLineLimit)
	if err != nil {
		return errorResult(err)
	}
	resolved, err := r.resolveExisting(path)
	if err != nil {
		return errorResult(err)
	}

	lines, totalLines, err := r.readWindow(ctx, resolved, offset, limit)
	if err != nil {
		return errorResult(fmt.Errorf("read %q: %w", path, err))
	}
	start := offset
	end := offset + len(lines) - 1
	if totalLines == 0 {
		start = 0
		end = 0
	}
	result := formatOutput(lines)
	if len(lines) > 0 && (start > 1 || end < totalLines) {
		result += fmt.Sprintf("\n\n[Read lines %d-%d of %d total]", start, end, totalLines)
	}
	return Result{
		Summary: fmt.Sprintf("Read %s (lines %d-%d of %d)", filepath.Base(path), start, end, totalLines),
		Content: result,
	}
}

func (r *Registry) glob(ctx context.Context, args map[string]json.RawMessage) Result {
	if err := validateKeys(args, "pattern"); err != nil {
		return errorResult(err)
	}
	pattern, err := stringArgument(args, "pattern", true)
	if err != nil {
		return errorResult(err)
	}
	pattern, err = r.workspacePattern(pattern)
	if err != nil {
		return errorResult(err)
	}
	paths := make([]string, 0, maxLineLimit)
	total := 0
	err = r.walkMatchingPaths(ctx, pattern, func(match string) error {
		total++
		if len(paths) < maxLineLimit {
			paths = append(paths, match)
		}
		return nil
	})
	if err != nil {
		return errorResult(fmt.Errorf("invalid glob pattern: %w", err))
	}
	sort.Strings(paths)
	return Result{
		Summary: fmt.Sprintf("Found %d files matching %q", total, pattern),
		Content: formatOutputWithTotal(paths, total),
	}
}

func (r *Registry) grep(ctx context.Context, args map[string]json.RawMessage) Result {
	if err := validateKeys(args, "pattern", "files"); err != nil {
		return errorResult(err)
	}
	pattern, err := stringArgument(args, "pattern", true)
	if err != nil {
		return errorResult(err)
	}
	files, err := stringArgument(args, "files", true)
	if err != nil {
		return errorResult(err)
	}
	matcher, err := regexp.Compile(pattern)
	if err != nil {
		return errorResult(fmt.Errorf("invalid regular expression: %w", err))
	}
	workspacePattern, err := r.workspacePattern(files)
	if err != nil {
		return errorResult(err)
	}
	results := make([]string, 0)
	total := 0
	err = r.walkMatchingPaths(ctx, workspacePattern, func(match string) error {
		resolved, err := filepath.EvalSymlinks(filepath.Join(r.root, match))
		if err != nil || !r.withinRoot(resolved) {
			return nil
		}
		info, err := os.Stat(resolved)
		if err != nil || info.IsDir() {
			return nil
		}
		relative := filepath.FromSlash(match)
		scanErr := scanLines(ctx, resolved, func(lineNumber int, line string) error {
			if matcher.MatchString(line) {
				total++
				if len(results) < maxLineLimit {
					results = append(results, fmt.Sprintf("%s:%d: %s", filepath.ToSlash(relative), lineNumber, truncateLine(line)))
				}
			}
			return nil
		})
		if scanErr != nil {
			return scanErr
		}
		return nil
	})
	if err != nil {
		return errorResult(fmt.Errorf("grep files: %w", err))
	}
	return Result{
		Summary: fmt.Sprintf("Found %d matches", total),
		Content: formatOutputWithTotal(results, total),
	}
}

func (r *Registry) gitDiff(ctx context.Context, args map[string]json.RawMessage) Result {
	if err := validateKeys(args, "staged", "path"); err != nil {
		return errorResult(err)
	}
	staged, err := boolArgument(args, "staged")
	if err != nil {
		return errorResult(err)
	}
	path, err := r.optionalGitPath(args)
	if err != nil {
		return errorResult(err)
	}
	command := []string{"diff", "--no-ext-diff"}
	if staged {
		command = append(command, "--cached")
	}
	command = append(command, "--")
	if path != "" {
		command = append(command, path)
	}
	return r.runGit(ctx, command, "Git diff")
}

func (r *Registry) gitLog(ctx context.Context, args map[string]json.RawMessage) Result {
	if err := validateKeys(args, "maxCount", "path"); err != nil {
		return errorResult(err)
	}
	count, err := intArgument(args, "maxCount", defaultLogCount, maxLogCount)
	if err != nil {
		return errorResult(err)
	}
	path, err := r.optionalGitPath(args)
	if err != nil {
		return errorResult(err)
	}
	command := []string{"log", "--no-decorate", "--oneline", "-n", strconv.Itoa(count)}
	if path != "" {
		command = append(command, "--", path)
	}
	return r.runGit(ctx, command, "Git log")
}

func (r *Registry) gitShow(ctx context.Context, args map[string]json.RawMessage) Result {
	if err := validateKeys(args, "revision", "path"); err != nil {
		return errorResult(err)
	}
	revision, err := stringArgument(args, "revision", true)
	if err != nil {
		return errorResult(err)
	}
	if strings.HasPrefix(revision, "-") {
		return errorResult(fmt.Errorf("revision must not start with '-'"))
	}
	path, err := r.optionalGitPath(args)
	if err != nil {
		return errorResult(err)
	}
	command := []string{"show", "--no-ext-diff", "--format=fuller", "--end-of-options", revision, "--"}
	if path != "" {
		command = append(command, path)
	}
	return r.runGit(ctx, command, "Git show")
}

func (r *Registry) git(ctx context.Context, args map[string]json.RawMessage, command ...string) Result {
	if len(args) != 0 {
		return errorResult(fmt.Errorf("tool does not accept arguments"))
	}
	return r.runGit(ctx, command, "Git status")
}

func (r *Registry) optionalGitPath(args map[string]json.RawMessage) (string, error) {
	path, err := stringArgument(args, "path", false)
	if err != nil || path == "" {
		return "", err
	}
	clean, err := r.resolveGitPath(path)
	if err != nil {
		return "", err
	}
	return clean, nil
}

func (r *Registry) runGit(parent context.Context, arguments []string, label string) Result {
	ctx, cancel := context.WithTimeout(parent, commandTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = r.root
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var stdout, stderr boundedBuffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}
	content := formatOutput(strings.Split(strings.TrimRight(output, "\n"), "\n"))
	if stdout.Truncated() || stderr.Truncated() {
		content += "\n\n[output truncated]"
	}
	if err != nil {
		if ctx.Err() != nil {
			return errorResult(fmt.Errorf("%s timed out: %w", label, ctx.Err()))
		}
		if content == "" {
			return errorResult(fmt.Errorf("%s failed: %w", label, err))
		}
		return Result{Summary: fmt.Sprintf("%s failed", label), Content: content}
	}
	return Result{Summary: label, Content: content}
}

type boundedBuffer struct {
	bytes.Buffer
	truncated bool
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	remaining := maxLineLimit*maxLineWidth - b.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(data), nil
	}
	if len(data) > remaining {
		_, _ = b.Buffer.Write(data[:remaining])
		b.truncated = true
		return len(data), nil
	}
	return b.Buffer.Write(data)
}

func (b *boundedBuffer) Truncated() bool { return b.truncated }

func (r *Registry) readWindow(ctx context.Context, path string, offset, limit int) ([]string, int, error) {
	lines := make([]string, 0, limit)
	total := 0
	err := scanLines(ctx, path, func(lineNumber int, line string) error {
		total = lineNumber
		if lineNumber >= offset && len(lines) < limit {
			lines = append(lines, truncateLine(line))
		}
		return nil
	})
	return lines, total, err
}

func scanLines(ctx context.Context, path string, visit func(int, string) error) (err error) {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		lineNumber++
		if err := visit(lineNumber, scanner.Text()); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (r *Registry) resolveExisting(path string) (string, error) {
	candidate, err := r.resolveLexical(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}
	if !r.withinRoot(resolved) {
		return "", fmt.Errorf("path %q is outside the workspace", path)
	}
	return resolved, nil
}

func (r *Registry) resolveGitPath(path string) (string, error) {
	candidate, err := r.resolveLexical(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(candidate); err == nil {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil || !r.withinRoot(resolved) {
			return "", fmt.Errorf("path %q is outside the workspace", path)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("path %q: %w", path, err)
	} else {
		parent := filepath.Dir(candidate)
		for {
			resolvedParent, resolveErr := filepath.EvalSymlinks(parent)
			if resolveErr == nil {
				if !r.withinRoot(resolvedParent) {
					return "", fmt.Errorf("path %q is outside the workspace", path)
				}
				break
			}
			next := filepath.Dir(parent)
			if next == parent {
				return "", fmt.Errorf("resolve path %q: %w", path, resolveErr)
			}
			parent = next
		}
	}
	relative, err := filepath.Rel(r.root, candidate)
	if err != nil {
		return "", fmt.Errorf("relative path %q: %w", path, err)
	}
	return filepath.ToSlash(relative), nil
}

func (r *Registry) workspacePattern(pattern string) (string, error) {
	if filepath.IsAbs(pattern) {
		if !r.withinRoot(filepath.Clean(pattern)) {
			return "", fmt.Errorf("pattern %q is outside the workspace", pattern)
		}
		relative, err := filepath.Rel(r.root, filepath.Clean(pattern))
		if err != nil {
			return "", fmt.Errorf("resolve pattern %q: %w", pattern, err)
		}
		return filepath.ToSlash(relative), nil
	}
	joined := filepath.Join(r.root, pattern)
	if !r.withinRoot(filepath.Clean(joined)) {
		return "", fmt.Errorf("pattern %q is outside the workspace", pattern)
	}
	return filepath.ToSlash(pattern), nil
}

func (r *Registry) walkMatchingPaths(ctx context.Context, pattern string, visit func(string) error) error {
	if err := validateGlobPattern(pattern); err != nil {
		return err
	}
	err := filepath.WalkDir(r.root, func(pathName string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(r.root, pathName)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		if !globMatch(pattern, relative) {
			return nil
		}
		resolved, err := filepath.EvalSymlinks(pathName)
		if err == nil && r.withinRoot(resolved) {
			return visit(relative)
		}
		return nil
	})
	return err
}

func validateKeys(args map[string]json.RawMessage, allowed ...string) error {
	known := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		known[key] = struct{}{}
	}
	for key := range args {
		if _, ok := known[key]; !ok {
			return fmt.Errorf("unknown argument %q", key)
		}
	}
	return nil
}

func validateGlobPattern(pattern string) error {
	for _, segment := range strings.Split(pattern, "/") {
		if segment == "**" {
			continue
		}
		if _, err := path.Match(segment, ""); err != nil {
			return err
		}
	}
	return nil
}

func globMatch(pattern, value string) bool {
	patternParts := strings.Split(strings.TrimPrefix(pattern, "./"), "/")
	valueParts := strings.Split(strings.TrimPrefix(value, "./"), "/")
	var match func(int, int) bool
	match = func(patternIndex, valueIndex int) bool {
		if patternIndex == len(patternParts) {
			return valueIndex == len(valueParts)
		}
		if patternParts[patternIndex] == "**" {
			if match(patternIndex+1, valueIndex) {
				return true
			}
			return valueIndex < len(valueParts) && match(patternIndex, valueIndex+1)
		}
		if valueIndex == len(valueParts) {
			return false
		}
		segmentMatch, err := path.Match(patternParts[patternIndex], valueParts[valueIndex])
		return err == nil && segmentMatch && match(patternIndex+1, valueIndex+1)
	}
	return match(0, 0)
}

func (r *Registry) resolveLexical(path string) (string, error) {
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(r.root, candidate)
	}
	candidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}
	if !r.withinRoot(candidate) {
		return "", fmt.Errorf("path %q is outside the workspace", path)
	}
	return candidate, nil
}

func (r *Registry) withinRoot(path string) bool {
	relative, err := filepath.Rel(r.root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func formatOutput(lines []string) string {
	return formatOutputWithTotal(lines, len(lines))
}

func formatOutputWithTotal(lines []string, total int) string {
	if len(lines) == 0 {
		return "(no output)"
	}
	if total <= len(lines) {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines, "\n") + fmt.Sprintf("\n\n[... %d more lines ...]", total-len(lines))
}

func truncateLine(line string) string {
	runes := []rune(line)
	if len(runes) <= maxLineWidth {
		return line
	}
	return string(runes[:maxLineWidth]) + "..."
}

func errorResult(err error) Result {
	message := "Error: " + err.Error()
	return Result{Summary: message, Content: message}
}
