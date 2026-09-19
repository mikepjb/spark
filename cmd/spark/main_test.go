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
		filepath.Join(workspace, ".agent", "skills"),
		filepath.Join(home, ".agents", "skills"),
		filepath.Join(home, ".agent", "skills"),
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
