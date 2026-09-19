package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

type Skill struct {
	Name        string
	Description string
	Path        string
}

type Catalog struct {
	items map[string]Skill
}

type metadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func Discover(roots []string) (*Catalog, error) {
	catalog := &Catalog{items: make(map[string]Skill)}
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read skills directory %q: %w", root, err)
		}
		for _, entry := range entries {
			skillDir := filepath.Join(root, entry.Name())
			info, err := os.Stat(skillDir)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("stat skill %q: %w", entry.Name(), err)
			}
			if !info.IsDir() {
				continue
			}
			path := filepath.Join(skillDir, "SKILL.md")
			data, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("read skill %q: %w", entry.Name(), err)
			}
			meta, _, err := parse(data)
			if err != nil {
				return nil, fmt.Errorf("parse skill %q: %w", entry.Name(), err)
			}
			if err := validateMetadata(meta, entry.Name()); err != nil {
				return nil, fmt.Errorf("validate skill %q: %w", entry.Name(), err)
			}
			name := meta.Name
			if _, exists := catalog.items[name]; exists {
				continue
			}
			catalog.items[name] = Skill{Name: name, Description: meta.Description, Path: path}
		}
	}
	return catalog, nil
}

func (c *Catalog) List() []Skill {
	if c == nil {
		return nil
	}
	result := make([]Skill, 0, len(c.items))
	for _, skill := range c.items {
		result = append(result, skill)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (c *Catalog) Load(name string) (Skill, string, error) {
	skill, ok := c.items[name]
	if !ok {
		return Skill{}, "", fmt.Errorf("skill %q is not available", name)
	}
	data, err := os.ReadFile(skill.Path)
	if err != nil {
		return Skill{}, "", fmt.Errorf("read skill %q: %w", name, err)
	}
	meta, body, err := parse(data)
	if err != nil {
		return Skill{}, "", fmt.Errorf("parse skill %q: %w", name, err)
	}
	if err := validateMetadata(meta, filepath.Base(filepath.Dir(skill.Path))); err != nil {
		return Skill{}, "", fmt.Errorf("validate skill %q: %w", name, err)
	}
	return skill, body, nil
}

func validateMetadata(meta metadata, directory string) error {
	if strings.TrimSpace(meta.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !validSkillName(meta.Name) {
		return fmt.Errorf("name must contain 1-64 lowercase letters, numbers, and single hyphens")
	}
	if meta.Name != directory {
		return fmt.Errorf("name %q must match directory %q", meta.Name, directory)
	}
	if strings.TrimSpace(meta.Description) == "" {
		return fmt.Errorf("description is required")
	}
	if utf8.RuneCountInString(meta.Description) > 1024 {
		return fmt.Errorf("description must be at most 1024 characters")
	}
	return nil
}

func validSkillName(name string) bool {
	if name == "" || len(name) > 64 || name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	previousHyphen := false
	for _, character := range name {
		if character == '-' {
			if previousHyphen {
				return false
			}
			previousHyphen = true
			continue
		}
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
		previousHyphen = false
	}
	return true
}

func parse(data []byte) (metadata, string, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return metadata{}, "", fmt.Errorf("missing front matter")
	}
	rest := strings.TrimPrefix(text, "---\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return metadata{}, "", fmt.Errorf("unterminated front matter")
	}
	var meta metadata
	if err := yaml.Unmarshal([]byte(rest[:end]), &meta); err != nil {
		return metadata{}, "", err
	}
	body := strings.TrimSpace(rest[end+len("\n---"):])
	body = strings.TrimPrefix(body, "\n")
	return meta, body, nil
}
