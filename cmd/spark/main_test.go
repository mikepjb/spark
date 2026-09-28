package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/mikepjb/spark/internal/llm"
	"github.com/mikepjb/spark/internal/repl"
	"github.com/mikepjb/spark/internal/skills"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type cliStream struct {
	deltas []llm.Delta
	index  int
	ctx    context.Context
	block  bool
	err    error
}

func (s *cliStream) Next() (llm.Delta, error) {
	if s.block {
		<-s.ctx.Done()
		return llm.Delta{}, s.ctx.Err()
	}
	if s.ctx != nil {
		select {
		case <-s.ctx.Done():
			return llm.Delta{}, s.ctx.Err()
		default:
		}
	}
	if s.index == len(s.deltas) {
		if s.err != nil {
			return llm.Delta{}, s.err
		}
		return llm.Delta{}, io.EOF
	}
	delta := s.deltas[s.index]
	s.index++
	return delta, nil
}

func (*cliStream) Close() error { return nil }

type cliClient struct {
	mu       sync.Mutex
	streams  [][]llm.Delta
	requests []llm.Request
	err      error
}

func (c *cliClient) Complete(ctx context.Context, request llm.Request) (llm.Stream, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, request)
	if c.err != nil {
		return nil, c.err
	}
	var deltas []llm.Delta
	if len(c.streams) > 0 {
		deltas, c.streams = c.streams[0], c.streams[1:]
	}
	return &cliStream{deltas: deltas, ctx: ctx}, nil
}

func (c *cliClient) request(index int) llm.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests[index]
}

func TestParseArgsDocumentedInvocations(t *testing.T) {
	cases := []struct {
		args []string
		want cliOptions
	}{
		{args: []string{"-p", "explain config flow"}, want: cliOptions{Prompt: "explain config flow", PromptSet: true, Headless: true}},
		{args: []string{"-p"}, want: cliOptions{PromptSet: true, PromptEditor: true, Headless: true}},
		{args: []string{"-s", "review"}, want: cliOptions{Skill: "review", Headless: true}},
		{args: []string{"-s", "review", "-p", "focus on concurrency"}, want: cliOptions{Prompt: "focus on concurrency", PromptSet: true, Skill: "review", Headless: true}},
		{args: []string{"--model", "qwen-4b", "-p", "review package"}, want: cliOptions{Prompt: "review package", PromptSet: true, Model: "qwen-4b", Headless: true}},
		{args: []string{"--format", "json", "-s", "review"}, want: cliOptions{Skill: "review", Format: "json", Headless: true}},
		{args: nil, want: cliOptions{}},
	}
	for _, test := range cases {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			got, err := parseArgs(test.args, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("options = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestParseArgsRejectsInvalidFlagsAndFormats(t *testing.T) {
	for _, args := range [][]string{{"--unknown"}, {"--format", "yaml"}, {"--format", "json"}, {"-p", ""}, {"-p", "ok", "extra"}} {
		if _, err := parseArgs(args, io.Discard); err == nil {
			t.Errorf("parseArgs(%q) succeeded, want error", args)
		}
	}
}

func TestRunUsesStableUsageAndConfigurationExitStatuses(t *testing.T) {
	var stdout, stderr strings.Builder
	if code := run([]string{"-p", ""}, nil, &stdout, &stderr); code != exitUsage {
		t.Fatalf("usage failure exit = %d, want %d; stderr=%q", code, exitUsage, stderr.String())
	}

	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".sparkrc"), []byte("queue_limit: 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	stdout.Reset()
	stderr.Reset()
	if code := run(nil, nil, &stdout, &stderr); code != exitRuntime {
		t.Fatalf("configuration failure exit = %d, want %d; stderr=%q", code, exitRuntime, stderr.String())
	}
}

func TestRunHeadlessTextStreamsAnswerAndKeepsDiagnosticsOffStdout(t *testing.T) {
	client := &cliClient{streams: [][]llm.Delta{{{Model: "resolved-qwen", Content: "answer", Usage: &llm.Usage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5}}}}}
	app := testApplication(t, client)
	var stdout, stderr strings.Builder
	code := runHeadless(context.Background(), app, cliOptions{Prompt: "question", Headless: true, Format: "text"}, strings.NewReader(""), &stdout, &stderr)
	app.coordinator.Close()
	if code != exitOK || stdout.String() != "answer\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "tool:") || stderr.Len() != 0 {
		t.Fatalf("unexpected diagnostics: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunHeadlessJSONEmitsVersionedResultAndToolTrace(t *testing.T) {
	client := &cliClient{streams: [][]llm.Delta{
		{{Model: "resolved-qwen", ToolCall: []llm.ToolCallDelta{{Index: 0, ID: "call-1", Name: "Read", Arguments: `{"filePath":"README.md"}`}}, Usage: &llm.Usage{PromptTokens: 4, CompletionTokens: 1, TotalTokens: 5}}},
		{{Content: "done", Usage: &llm.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12}}},
	}}
	app := testApplication(t, client)
	var stdout, stderr strings.Builder
	code := runHeadless(context.Background(), app, cliOptions{Prompt: "question", Headless: true, Format: "json"}, nil, &stdout, &stderr)
	app.coordinator.Close()
	if code != exitOK {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	var result resultDocument
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("decode result: %v; output=%q", err, stdout.String())
	}
	if result.SchemaVersion != 1 || result.SelectedProfile != "qwen-profile" || result.ResolvedModel != "resolved-qwen" || result.Answer != "done" || result.Status != "succeeded" || result.Usage.TotalTokens != 17 || len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "Read" {
		t.Fatalf("result = %+v", result)
	}
	if strings.Contains(stdout.String(), "tool:") || !strings.Contains(stderr.String(), "tool: Read") {
		t.Fatalf("stdout/stderr routing incorrect: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunHeadlessPreservesPartialStreamOnModelFailure(t *testing.T) {
	client := &partialFailureClient{}
	app := testApplication(t, client)
	var stdout, stderr strings.Builder
	code := runHeadless(context.Background(), app, cliOptions{Prompt: "question", Headless: true, Format: "json"}, nil, &stdout, &stderr)
	app.coordinator.Close()
	var result resultDocument
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("decode result: %v; output=%q", err, stdout.String())
	}
	if code != exitRuntime || result.Status != "failed" || result.Answer != "partial" || !strings.Contains(result.Error, "stream broke") {
		t.Fatalf("code=%d result=%+v stderr=%q", code, result, stderr.String())
	}
}

type partialFailureClient struct{}

func (*partialFailureClient) Complete(ctx context.Context, _ llm.Request) (llm.Stream, error) {
	return &cliStream{ctx: ctx, deltas: []llm.Delta{{Content: "partial"}}, err: errors.New("stream broke")}, nil
}

func TestRunHeadlessSkillUsesDocumentedWorkspacePrompt(t *testing.T) {
	client := &cliClient{streams: [][]llm.Delta{{{Content: "done"}}}}
	app := testApplication(t, client)
	root := t.TempDir()
	skillDir := filepath.Join(root, "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Review code\n---\nInspect changed code carefully."), 0o600); err != nil {
		t.Fatal(err)
	}
	app.skillCatalog, _ = skills.Discover([]string{root})
	var stdout, stderr strings.Builder
	code := runHeadless(context.Background(), app, cliOptions{Skill: "review", Headless: true, Format: "text"}, nil, &stdout, &stderr)
	app.coordinator.Close()
	if code != exitOK {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	content := client.request(0).Messages[len(client.request(0).Messages)-1].Content
	if content == nil || !strings.Contains(*content, "Apply the selected skill to this workspace") || !strings.Contains(*content, "Inspect changed code carefully") {
		t.Fatalf("submitted content = %v", content)
	}
}

func TestRunHeadlessUsesEditorPromptAndKeepsEditorOutputOffStdout(t *testing.T) {
	client := &cliClient{streams: [][]llm.Delta{{{Content: "answer"}}}}
	app := testApplication(t, client)
	editor := filepath.Join(t.TempDir(), "editor")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\necho editor-ui\nprintf 'review the config flow' > \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", editor)
	var stdout, stderr strings.Builder
	code := runHeadless(context.Background(), app, cliOptions{PromptSet: true, PromptEditor: true, Headless: true, Format: "json"}, strings.NewReader(""), &stdout, &stderr)
	app.coordinator.Close()
	if code != exitOK || strings.Contains(stdout.String(), "editor-ui") || !strings.Contains(stderr.String(), "editor-ui") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var result resultDocument
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("decode result: %v; output=%q", err, stdout.String())
	}
	request := client.request(0)
	if result.Answer != "answer" || request.Messages[len(request.Messages)-1].Content == nil || *request.Messages[len(request.Messages)-1].Content != "review the config flow" {
		t.Fatalf("result=%+v request=%+v", result, request)
	}
}

func TestRunHeadlessRejectsEmptyEditorPrompt(t *testing.T) {
	app := testApplication(t, &cliClient{})
	editor := filepath.Join(t.TempDir(), "editor")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", editor)
	var stdout, stderr strings.Builder
	code := runHeadless(context.Background(), app, cliOptions{PromptEditor: true, Headless: true, Format: "json"}, strings.NewReader(""), &stdout, &stderr)
	app.coordinator.Close()
	if code != exitUsage || stdout.Len() != 0 || !strings.Contains(stderr.String(), "prompt file is empty") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunHeadlessReportsFailureAndCancellationStatuses(t *testing.T) {
	t.Run("failure", func(t *testing.T) {
		app := testApplication(t, &cliClient{err: errors.New("model unavailable")})
		var stdout, stderr strings.Builder
		code := runHeadless(context.Background(), app, cliOptions{Prompt: "question", Headless: true, Format: "json"}, nil, &stdout, &stderr)
		app.coordinator.Close()
		if code != exitRuntime || !strings.Contains(stdout.String(), `"status":"failed"`) || !strings.Contains(stdout.String(), "model unavailable") {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	})
	t.Run("cancel", func(t *testing.T) {
		client := &blockingCLIClient{}
		app := testApplication(t, client)
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(10*time.Millisecond, cancel)
		var stdout, stderr strings.Builder
		code := runHeadless(ctx, app, cliOptions{Prompt: "question", Headless: true, Format: "json"}, nil, &stdout, &stderr)
		cancel()
		app.coordinator.Close()
		if code != exitInterrupted || !strings.Contains(stdout.String(), `"status":"cancelled"`) {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	})
}

type blockingCLIClient struct{}

func (*blockingCLIClient) Complete(ctx context.Context, _ llm.Request) (llm.Stream, error) {
	return &cliStream{ctx: ctx, block: true}, nil
}

func testApplication(t *testing.T, client llm.Client) *application {
	t.Helper()
	coordinator := repl.New(client, repl.Config{Model: "resolved-qwen", Tools: nil})
	return &application{coordinator: coordinator, modelName: "resolved-qwen", profileName: "qwen-profile", skillCatalog: &skills.Catalog{}}
}

func TestSkillRootsUseProjectAndGlobalConventionsByDefault(t *testing.T) {
	workspace := filepath.Join("tmp", "workspace")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	roots, err := skillRoots(workspace, nil)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		filepath.Join(workspace, ".agents", "skills"),
		filepath.Join(home, ".agents", "skills"),
	}
	if !reflect.DeepEqual(roots, want) {
		t.Fatalf("roots = %v, want %v", roots, want)
	}
}

func TestSkillRootsUseConfiguredPathsAsOverride(t *testing.T) {
	configured := []string{".custom/skills", "/tmp/global-skills"}
	roots, err := skillRoots("/workspace", configured)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{filepath.Join("/workspace", ".custom/skills"), "/tmp/global-skills"}
	if !reflect.DeepEqual(roots, want) {
		t.Fatalf("roots = %v, want %v", roots, want)
	}
}

func TestUserAgentPromptPrefersGenericPath(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".agents", "AGENTS.md"), []byte("generic guidance\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := userAgentPromptFromHome(home)
	if err != nil {
		t.Fatal(err)
	}
	if got != "generic guidance" {
		t.Fatalf("guidance = %q", got)
	}
}

func TestUserAgentPromptIgnoresCodexPath(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "AGENTS.md"), []byte("codex guidance\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := userAgentPromptFromHome(home)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("guidance = %q, want empty", got)
	}
}

func TestUserAgentPromptAllowsMissingFile(t *testing.T) {
	got, err := userAgentPromptFromHome(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("guidance = %q, want empty", got)
	}
}

func TestSameDirectory(t *testing.T) {
	home := t.TempDir()

	if !sameDirectory(home, home) {
		t.Fatal("expected the home directory to be recognized")
	}

	project := filepath.Join(home, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if sameDirectory(project, home) {
		t.Fatal("did not expect a child directory to be recognized as home")
	}
}

func TestSameDirectoryFollowsSymlinks(t *testing.T) {
	home := t.TempDir()
	link := filepath.Join(t.TempDir(), "home-link")
	if err := os.Symlink(home, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if !sameDirectory(link, home) {
		t.Fatal("expected a symlink to the home directory to be recognized")
	}
}
