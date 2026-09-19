package files

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestListReturnsRelativeRegularFilesAndSkipsGit(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"src/main.go", ".hidden", ".git/config", "nested/.hidden/file.txt"} {
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
	if len(got) != 1 || got[0] != "src/main.go" {
		t.Fatalf("files = %#v", got)
	}
}

func TestListSkipsGitIgnoredFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored.txt\nignored-dir/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"kept.txt", "ignored.txt", "ignored-dir/file.txt"} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}

	got, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "kept.txt" {
		t.Fatalf("files = %#v", got)
	}
}
