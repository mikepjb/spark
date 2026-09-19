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
