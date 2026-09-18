package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikepjb/spark/internal/llm"
)

func TestRegistryDefinitionsAndWorkspaceTools(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("first\nneedle here\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	registry, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	definitions := registry.Definitions()
	if len(definitions) != 7 {
		t.Fatalf("definition count = %d, want 7", len(definitions))
	}

	read := registry.Execute(context.Background(), call("Read", `{"filePath":"notes.txt","offset":2}`))
	if !strings.Contains(read.Content, "needle here") {
		t.Fatalf("read result = %q", read.Content)
	}

	glob := registry.Execute(context.Background(), call("Glob", `{"pattern":"src/*.go"}`))
	if glob.Content != "src/main.go" {
		t.Fatalf("glob result = %q", glob.Content)
	}

	grep := registry.Execute(context.Background(), call("Grep", `{"pattern":"needle","files":"**/*.txt"}`))
	if !strings.Contains(grep.Content, "notes.txt:2: needle here") {
		t.Fatalf("grep result = %q", grep.Content)
	}

	escape := registry.Execute(context.Background(), call("Read", `{"filePath":"../outside.txt"}`))
	if !strings.Contains(escape.Content, "outside the workspace") {
		t.Fatalf("escape result = %q", escape.Content)
	}
	invalidGlob := registry.Execute(context.Background(), call("Glob", `{"pattern":"["}`))
	if !strings.Contains(invalidGlob.Content, "invalid glob pattern") {
		t.Fatalf("invalid glob result = %q", invalidGlob.Content)
	}
}

func TestRegistryBoundsReadOutput(t *testing.T) {
	root := t.TempDir()
	lines := make([]string, maxLineLimit+25)
	for i := range lines {
		lines[i] = "line"
	}
	path := filepath.Join(root, "large.txt")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	result := registry.Execute(context.Background(), call("Read", `{"filePath":"large.txt"}`))
	if !strings.Contains(result.Content, "[Read lines 1-500 of 525 total]") {
		t.Fatalf("bounded read result did not describe truncation: %q", result.Content)
	}
}

func TestRegistryGitTools(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Spark Test", "GIT_AUTHOR_EMAIL=spark@example.invalid",
			"GIT_COMMITTER_NAME=Spark Test", "GIT_COMMITTER_EMAIL=spark@example.invalid",
		)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gone.txt"), []byte("gone\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "tracked.txt", "gone.txt")
	git("commit", "-qm", "initial")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "gone.txt")); err != nil {
		t.Fatal(err)
	}

	registry, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		tool string
		args string
		want string
	}{
		{"status", "GitStatus", `{}`, "tracked.txt"},
		{"diff", "GitDiff", `{"path":"tracked.txt"}`, "+two"},
		{"log", "GitLog", `{"maxCount":1}`, "initial"},
		{"show", "GitShow", `{"revision":"HEAD","path":"tracked.txt"}`, "one"},
		{"show deleted", "GitShow", `{"revision":"HEAD","path":"gone.txt"}`, "gone"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := registry.Execute(context.Background(), call(test.tool, test.args))
			if !strings.Contains(result.Content, test.want) {
				t.Fatalf("%s result = %q, want %q", test.name, result.Content, test.want)
			}
		})
	}
}

func call(name, args string) llm.ToolCall {
	return llm.ToolCall{Name: name, Arguments: json.RawMessage(args)}
}
