package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

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
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".agents", "AGENTS.md"), []byte("generic guidance\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "AGENTS.md"), []byte("codex guidance\n"), 0o600); err != nil {
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

func TestUserAgentPromptFallsBackToCodexPath(t *testing.T) {
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
	if got != "codex guidance" {
		t.Fatalf("guidance = %q", got)
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
