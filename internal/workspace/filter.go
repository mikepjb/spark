package workspace

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
)

// IgnoreMatcher contains paths reported by Git as ignored and untracked.
// An empty matcher is valid for non-Git workspaces or when Git is unavailable.
type IgnoreMatcher struct {
	paths map[string]struct{}
}

func NewIgnoreMatcher(ctx context.Context, root string) IgnoreMatcher {
	command := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	output, err := command.Output()
	if err != nil {
		return IgnoreMatcher{}
	}

	paths := make(map[string]struct{})
	for _, path := range strings.Split(string(output), "\x00") {
		path = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(path)), "/")
		if path != "" && path != "." {
			paths[path] = struct{}{}
		}
	}
	return IgnoreMatcher{paths: paths}
}

func (m IgnoreMatcher) Match(path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	for path != "" && path != "." {
		if _, ok := m.paths[path]; ok {
			return true
		}
		parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(path)))
		if parent == path {
			break
		}
		path = parent
	}
	return false
}

func IsHidden(path string) bool {
	for _, component := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.HasPrefix(component, ".") && component != "." && component != ".." {
			return true
		}
	}
	return false
}
