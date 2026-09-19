package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(root, entry.Name(), "SKILL.md")
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
			name := meta.Name
			if name == "" {
				name = entry.Name()
			}
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
	_, body, err := parse(data)
	if err != nil {
		return Skill{}, "", fmt.Errorf("parse skill %q: %w", name, err)
	}
	return skill, body, nil
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
