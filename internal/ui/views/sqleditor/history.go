// Package sqleditor provides the SQL Editor view for Steep.
package sqleditor

import (
	"strings"
	"sync"

	"github.com/willibrandon/steep/internal/storage/sqlite"
)

// HistoryManager manages query history with in-memory cache backed by SQLite.
type HistoryManager struct {
	store        *sqlite.HistoryStore
	cache        []HistoryEntry // In-memory cache for fast navigation
	currentIndex int            // Current position in history (-1 = not browsing)
	maxEntries   int            // Maximum entries in cache
	mu           sync.RWMutex
}

// NewHistoryManager creates a new history manager using the shared database.
func NewHistoryManager(db *sqlite.DB, maxEntries int) *HistoryManager {
	if maxEntries <= 0 {
		maxEntries = MaxHistoryEntries
	}

	hm := &HistoryManager{
		store:        sqlite.NewHistoryStore(db),
		cache:        make([]HistoryEntry, 0, maxEntries),
		currentIndex: -1,
		maxEntries:   maxEntries,
	}

	// Load recent history into cache
	hm.loadCache()

	return hm
}

// loadCache loads recent history entries from SQLite into the in-memory cache.
func (hm *HistoryManager) loadCache() {
	if hm.store == nil {
		return
	}

	entries, err := hm.store.GetRecent(hm.maxEntries)
	if err != nil {
		return
	}

	hm.mu.Lock()
	defer hm.mu.Unlock()

	// Convert from storage type to local type and reverse order
	// (so oldest is first, newest is last for navigation)
	hm.cache = make([]HistoryEntry, len(entries))
	for i, entry := range entries {
		hm.cache[len(entries)-1-i] = HistoryEntry{
			ID:         entry.ID,
			SQL:        entry.SQL,
			ExecutedAt: entry.ExecutedAt,
			DurationMs: entry.DurationMs,
			RowCount:   entry.RowCount,
			Error:      entry.Error,
		}
	}
}

// Add adds a query to history with shell-style deduplication.
// If a query with the same fingerprint exists, it moves to the top.
// Otherwise, a new entry is added.
func (hm *HistoryManager) Add(sql string, durationMs, rowCount int64, queryError string) {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return
	}

	// Persist to SQLite (handles fingerprint-based deduplication)
	if hm.store != nil {
		if err := hm.store.Add(sql, durationMs, rowCount, queryError); err != nil {
			return
		}
	}

	// Reload cache from SQLite to stay in sync (shell-style order)
	hm.loadCache()

	// Reset navigation index
	hm.mu.Lock()
	hm.currentIndex = -1
	hm.mu.Unlock()
}

// Previous returns the previous query in history.
// Returns empty string if at the beginning or history is empty.
func (hm *HistoryManager) Previous() string {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	if len(hm.cache) == 0 {
		return ""
	}

	if hm.currentIndex == -1 {
		// Start from the end (most recent)
		hm.currentIndex = len(hm.cache) - 1
	} else if hm.currentIndex > 0 {
		hm.currentIndex--
	}

	return hm.cache[hm.currentIndex].SQL
}

// Next returns the next query in history (more recent).
// Returns empty string if at the end or past the end.
func (hm *HistoryManager) Next() string {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	if len(hm.cache) == 0 || hm.currentIndex == -1 {
		return ""
	}

	if hm.currentIndex < len(hm.cache)-1 {
		hm.currentIndex++
		return hm.cache[hm.currentIndex].SQL
	}

	// Past the end - return empty to allow clearing editor
	hm.currentIndex = -1
	return ""
}

// Current returns the current history entry being viewed.
// Returns nil if not browsing history.
func (hm *HistoryManager) Current() *HistoryEntry {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	if hm.currentIndex < 0 || hm.currentIndex >= len(hm.cache) {
		return nil
	}

	entry := hm.cache[hm.currentIndex]
	return &entry
}

// ResetNavigation resets the history navigation position.
func (hm *HistoryManager) ResetNavigation() {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.currentIndex = -1
}

// IsBrowsing returns true if currently navigating history.
func (hm *HistoryManager) IsBrowsing() bool {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return hm.currentIndex >= 0
}

// Search returns entries matching the search query (case-insensitive).
// Results are ordered from most recent to oldest.
func (hm *HistoryManager) Search(query string) []HistoryEntry {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	if query == "" {
		// Return all entries (most recent first)
		result := make([]HistoryEntry, len(hm.cache))
		for i, entry := range hm.cache {
			result[len(hm.cache)-1-i] = entry
		}
		return result
	}

	queryLower := strings.ToLower(query)
	var results []HistoryEntry

	// Search from most recent to oldest
	for i := len(hm.cache) - 1; i >= 0; i-- {
		if strings.Contains(strings.ToLower(hm.cache[i].SQL), queryLower) {
			results = append(results, hm.cache[i])
		}
	}

	return results
}

// Len returns the number of entries in the cache.
func (hm *HistoryManager) Len() int {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return len(hm.cache)
}
