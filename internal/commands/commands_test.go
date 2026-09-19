package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikepjb/spark/internal/skills"
)

func testEngine(t *testing.T, selectModel func(string) (string, error)) *Engine {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "analyse", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: analyse\ndescription: inspect\n---\nSkill body"), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := skills.Discover([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	return New(catalog, []ModelOption{{Name: "local", Provider: "local", Model: "qwen"}}, []string{"src/main.go", "README.md"}, selectModel)
}

func TestCandidatesUseFuzzyCommandAndFileCompletion(t *testing.T) {
	engine := testEngine(t, nil)
	commands := engine.Candidates("/anal", 5)
	if len(commands.Items) != 1 || commands.Items[0].Text != "/analyse" {
		t.Fatalf("commands = %+v", commands)
	}
	files := engine.Candidates("look @main", len([]rune("look @main")))
	if len(files.Items) != 1 || files.Items[0].Text != "@src/main.go" {
		t.Fatalf("files = %+v", files)
	}
	skills := engine.Candidates("use $", len([]rune("use $")))
	if len(skills.Items) != 1 || skills.Items[0].Text != "$analyse" {
		t.Fatalf("skills = %+v", skills)
	}
}

func TestPrepareLoadsSlashAndDollarSkills(t *testing.T) {
	engine := testEngine(t, nil)
	prepared, err := engine.Prepare("/analyse inspect $analyse")
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Submission == nil || prepared.Submission.Prompt != "inspect" || len(prepared.Submission.Skills) != 2 {
		t.Fatalf("prepared = %+v", prepared)
	}
}

func TestPreparePreservesOrdinaryPromptWhitespace(t *testing.T) {
	engine := testEngine(t, nil)
	prepared, err := engine.Prepare("first line\n\n  second line")
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Submission == nil || prepared.Submission.Prompt != "first line\n\n  second line" {
		t.Fatalf("prepared = %+v", prepared)
	}
}

func TestPrepareSelectsModelLocally(t *testing.T) {
	selected := ""
	engine := testEngine(t, func(name string) (string, error) {
		selected = name
		return "qwen", nil
	})
	prepared, err := engine.Prepare("/model local")
	if err != nil || prepared.Local != "selected model: qwen" || selected != "local" {
		t.Fatalf("prepared = %+v selected = %q err = %v", prepared, selected, err)
	}
}

func TestApplyPreservesTextAfterToken(t *testing.T) {
	token, ok := ActiveToken("use @main.go now", 12)
	if !ok {
		t.Fatal("expected active file token")
	}
	got, cursor := Apply("use @main.go now", token, Candidate{Text: "@src/main.go"})
	if got != "use @src/main.go now" || cursor != len([]rune("use @src/main.go")) {
		t.Fatalf("result = %q cursor = %d", got, cursor)
	}
}
