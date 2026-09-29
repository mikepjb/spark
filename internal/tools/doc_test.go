package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikepjb/spark/internal/llm"
)

func TestDocToolUsesBoundedScriptWithoutShell(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "doc")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	registry, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	definitions := registry.Definitions()
	if definitions[len(definitions)-1].Function.Name != "Doc" {
		t.Fatalf("Doc definition is absent: %+v", definitions)
	}
	result := registry.Execute(context.Background(), call("Doc", `{"action":"show","language":"go","entry":"encoding/json","symbol":"Decoder"}`))
	if result.Failed || result.Content != strings.Join([]string{dir, "show", "go", "encoding/json", "Decoder"}, "\n") {
		t.Fatalf("Doc show = %+v", result)
	}
	result = registry.Execute(context.Background(), call("Doc", `{"action":"search","language":"go","pattern":"x; touch /tmp/nope","scope":"std"}`))
	if result.Failed || !strings.Contains(result.Content, "x; touch /tmp/nope") {
		t.Fatalf("Doc search = %+v", result)
	}
}

func TestDocToolRejectsUnsafeAndInvalidOperations(t *testing.T) {
	registry := &Registry{root: t.TempDir(), docPath: "/bin/true"}
	for _, args := range []string{
		`{"action":"members","language":"python","entry":"os"}`,
		`{"action":"members","language":"node","entry":"fs"}`,
		`{"action":"show","language":"python","entry":"os"}`,
		`{"action":"show","language":"go","entry":"-help"}`,
		`{"action":"show","language":"ts","entry":"../../secret"}`,
		`{"action":"list","language":"go","scope":"project"}`,
		`{"action":"search","language":"ts","pattern":"foo","scope":"std"}`,
		`{"action":"search","language":"go","pattern":"foo","extra":true}`,
	} {
		result := registry.Execute(context.Background(), call("Doc", args))
		if !result.Failed {
			t.Errorf("accepted %s: %+v", args, result)
		}
	}
}

func TestDocToolTruncatesLargeOutput(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "doc")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ni=0\nwhile [ $i -lt 200 ]; do printf '%0200d\\n' 0; i=$((i+1)); done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	registry := &Registry{root: dir, docPath: script}
	result := registry.Execute(context.Background(), call("Doc", `{"action":"list","language":"go"}`))
	if result.Failed || !strings.Contains(result.Content, "[documentation output truncated]") || len(result.Content) > maxDocOutput+100 {
		t.Fatalf("truncation failed: %+v", result)
	}
}

func TestDocSearchNoMatches(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "doc")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	registry := &Registry{root: dir, docPath: script}
	result := registry.Execute(context.Background(), call("Doc", `{"action":"search","language":"go","pattern":"absent"}`))
	if result.Failed || result.Content != "[no documentation found]" {
		t.Fatalf("no-match result = %+v", result)
	}
}

func TestDocDefinitionHiddenWhenUnavailable(t *testing.T) {
	registry := &Registry{root: t.TempDir()}
	for _, definition := range registry.Definitions() {
		if definition.Function.Name == "Doc" {
			t.Fatal("Doc advertised without executable")
		}
	}
	if result := registry.Execute(context.Background(), llm.ToolCall{Name: "Doc", Arguments: []byte(`{}`)}); !result.Failed {
		t.Fatalf("Doc executed without executable: %+v", result)
	}
}
