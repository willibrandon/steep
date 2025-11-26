// Package sqleditor provides the SQL Editor view for Steep.
package sqleditor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// SnippetsFile represents the YAML file structure for storing snippets.
type SnippetsFile struct {
	Version  int       `yaml:"version"`
	Snippets []Snippet `yaml:"snippets"`
}

// SnippetManager manages query snippets with YAML persistence.
type SnippetManager struct {
	snippets map[string]*Snippet
	filePath string
	mu       sync.RWMutex
}

// NewSnippetManager creates a new snippet manager.
// Snippets are stored in ~/.config/steep/snippets.yaml
func NewSnippetManager() (*SnippetManager, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}

	steepDir := filepath.Join(configDir, "steep")
	if err := os.MkdirAll(steepDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	sm := &SnippetManager{
		snippets: make(map[string]*Snippet),
		filePath: filepath.Join(steepDir, "snippets.yaml"),
	}

	// Load existing snippets
	if err := sm.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load snippets: %w", err)
	}

	return sm, nil
}

// load reads snippets from the YAML file.
func (sm *SnippetManager) load() error {
	data, err := os.ReadFile(sm.filePath)
	if err != nil {
		return err
	}

	var file SnippetsFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("failed to parse snippets file: %w", err)
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.snippets = make(map[string]*Snippet)
	for i := range file.Snippets {
		snippet := file.Snippets[i]
		sm.snippets[snippet.Name] = &snippet
	}

	return nil
}

// save writes snippets to the YAML file.
func (sm *SnippetManager) save() error {
	sm.mu.RLock()
	snippets := make([]Snippet, 0, len(sm.snippets))
	for _, s := range sm.snippets {
		snippets = append(snippets, *s)
	}
	sm.mu.RUnlock()

	// Sort by name for consistent ordering
	sort.Slice(snippets, func(i, j int) bool {
		return snippets[i].Name < snippets[j].Name
	})

	file := SnippetsFile{
		Version:  1,
		Snippets: snippets,
	}

	data, err := yaml.Marshal(&file)
	if err != nil {
		return fmt.Errorf("failed to marshal snippets: %w", err)
	}

	if err := os.WriteFile(sm.filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write snippets file: %w", err)
	}

	return nil
}

// Save saves a query as a named snippet.
// Returns true if an existing snippet was overwritten.
func (sm *SnippetManager) Save(name, sql, description string) (overwritten bool, err error) {
	name = strings.TrimSpace(name)
	sql = strings.TrimSpace(sql)

	if name == "" {
		return false, fmt.Errorf("snippet name cannot be empty")
	}
	if sql == "" {
		return false, fmt.Errorf("snippet SQL cannot be empty")
	}

	// Validate name (alphanumeric, dash, underscore, dot only)
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return false, fmt.Errorf("snippet name can only contain letters, numbers, dashes, underscores, and dots")
		}
	}

	sm.mu.Lock()
	existing, exists := sm.snippets[name]
	now := time.Now()

	if exists {
		existing.SQL = sql
		existing.Description = description
		existing.UpdatedAt = now
		overwritten = true
	} else {
		sm.snippets[name] = &Snippet{
			Name:        name,
			SQL:         sql,
			Description: description,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
	}
	sm.mu.Unlock()

	if err := sm.save(); err != nil {
		return false, err
	}

	return overwritten, nil
}

// Exists checks if a snippet with the given name exists.
func (sm *SnippetManager) Exists(name string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	_, exists := sm.snippets[name]
	return exists
}

// Load retrieves a snippet by name.
func (sm *SnippetManager) Load(name string) (*Snippet, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("snippet name cannot be empty")
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	snippet, exists := sm.snippets[name]
	if !exists {
		return nil, fmt.Errorf("snippet '%s' not found", name)
	}

	// Return a copy
	copy := *snippet
	return &copy, nil
}

// Delete removes a snippet by name.
func (sm *SnippetManager) Delete(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("snippet name cannot be empty")
	}

	sm.mu.Lock()
	if _, exists := sm.snippets[name]; !exists {
		sm.mu.Unlock()
		return fmt.Errorf("snippet '%s' not found", name)
	}
	delete(sm.snippets, name)
	sm.mu.Unlock()

	return sm.save()
}

// List returns all saved snippets sorted by name.
func (sm *SnippetManager) List() []Snippet {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	snippets := make([]Snippet, 0, len(sm.snippets))
	for _, s := range sm.snippets {
		snippets = append(snippets, *s)
	}

	sort.Slice(snippets, func(i, j int) bool {
		return snippets[i].Name < snippets[j].Name
	})

	return snippets
}

// Count returns the number of saved snippets.
func (sm *SnippetManager) Count() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.snippets)
}

// Search returns snippets matching the query (case-insensitive search in name and SQL).
func (sm *SnippetManager) Search(query string) []Snippet {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return sm.List()
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var results []Snippet
	for _, s := range sm.snippets {
		if strings.Contains(strings.ToLower(s.Name), query) ||
			strings.Contains(strings.ToLower(s.SQL), query) ||
			strings.Contains(strings.ToLower(s.Description), query) {
			results = append(results, *s)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})

	return results
}
