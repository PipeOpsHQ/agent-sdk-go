package prompt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func LoadDir(path string) (int, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(entry.Name()))
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		fullPath := filepath.Join(path, entry.Name())
		spec, err := loadFile(fullPath)
		if err != nil {
			return loaded, err
		}
		if err := Register(spec); err != nil {
			return loaded, err
		}
		loaded++
	}
	return loaded, nil
}

func loadFile(path string) (Spec, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Spec{}, fmt.Errorf("read prompt file %q: %w", path, err)
	}
	var spec Spec
	if err := json.Unmarshal(content, &spec); err != nil {
		return Spec{}, fmt.Errorf("decode prompt file %q: %w", path, err)
	}
	if strings.TrimSpace(spec.Name) == "" {
		base := filepath.Base(path)
		spec.Name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return spec, nil
}
