package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverAndLoad(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "analyse", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: analyse\ndescription: inspect code\n---\n\nUse evidence.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	catalog, err := Discover([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	items := catalog.List()
	if len(items) != 1 || items[0].Name != "analyse" || items[0].Description != "inspect code" {
		t.Fatalf("unexpected skills: %+v", items)
	}
	_, body, err := catalog.Load("analyse")
	if err != nil || body != "Use evidence." {
		t.Fatalf("load = %q, %v", body, err)
	}
}

func TestDiscoverFollowsSymlinkedSkillDirectory(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	targetPath := filepath.Join(target, "analyse", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("---\nname: analyse\ndescription: inspect code\n---\nUse evidence."), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(targetPath), filepath.Join(root, "analyse")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	catalog, err := Discover([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if items := catalog.List(); len(items) != 1 || items[0].Name != "analyse" {
		t.Fatalf("unexpected skills: %+v", items)
	}
}

func TestDiscoverRejectsInvalidMetadata(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing name",
			body: "---\ndescription: inspect code\n---\nUse evidence.",
		},
		{
			name: "missing description",
			body: "---\nname: analyse\n---\nUse evidence.",
		},
		{
			name: "name does not match directory",
			body: "---\nname: other\ndescription: inspect code\n---\nUse evidence.",
		},
		{
			name: "invalid name",
			body: "---\nname: Analyse\ndescription: inspect code\n---\nUse evidence.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "analyse", "SKILL.md")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(test.body), 0o600); err != nil {
				t.Fatal(err)
			}

			if _, err := Discover([]string{root}); err == nil {
				t.Fatal("expected invalid skill metadata error")
			}
		})
	}
}

func TestDiscoverUsesFirstRootForDuplicateSkillNames(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeSkill := func(root, body string) {
		t.Helper()
		path := filepath.Join(root, "analyse", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		contents := "---\nname: analyse\ndescription: inspect code\n---\n" + body
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill(first, "first")
	writeSkill(second, "second")

	catalog, err := Discover([]string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	_, body, err := catalog.Load("analyse")
	if err != nil || body != "first" {
		t.Fatalf("body = %q, err = %v", body, err)
	}
}
