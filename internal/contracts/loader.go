// Package contracts loads Den Memories runtime-neutral contract artifacts.
package contracts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Loader reads v0 contract artifacts from the repository root.
type Loader struct {
	root string
}

// NewLoader creates a contract artifact loader.
func NewLoader(root string) *Loader {
	if root == "" {
		root = "."
	}
	return &Loader{root: root}
}

// Registry loads contracts/v0/registry.json.
func (l *Loader) Registry() (map[string]any, error) {
	return l.readJSON("contracts", "v0", "registry.json")
}

// ScoringDefaults loads contracts/v0/scoring-defaults.json.
func (l *Loader) ScoringDefaults() (map[string]any, error) {
	return l.readJSON("contracts", "v0", "scoring-defaults.json")
}

func (l *Loader) readJSON(parts ...string) (map[string]any, error) {
	path := filepath.Join(append([]string{l.root}, parts...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return result, nil
}
