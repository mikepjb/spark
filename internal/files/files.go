package files

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"

	"github.com/mikepjb/spark/internal/workspace"
)

func List(root string) ([]string, error) {
	ignored := workspace.NewIgnoreMatcher(context.Background(), root)
	var result []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("make file path relative: %w", err)
		}
		relative = filepath.ToSlash(relative)
		if workspace.IsHidden(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if ignored.Match(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		result = append(result, relative)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list workspace files: %w", err)
	}
	sort.Strings(result)
	return result, nil
}
