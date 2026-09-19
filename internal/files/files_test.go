package files

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListReturnsRelativeRegularFilesAndSkipsGit(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"src/main.go", ".hidden", ".git/config"} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != ".hidden" || got[1] != "src/main.go" {
		t.Fatalf("files = %#v", got)
	}
}
