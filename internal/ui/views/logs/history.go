package logs

import (
	"strings"
	"sync"

	"github.com/willibrandon/steep/internal/storage/sqlite"
)

// MaxHistoryEntries is the maximum number of history entries to keep in cache.
const MaxHistoryEntries = 500

// HistoryManager manages command and search history with in-memory cache backed by SQLite.
type HistoryManager struct {
	store        *sqlite.LogHistoryStore
	commands     []string // In-memory cache for command history
	searches     []string // In-memory cache for search history
	commandIndex int      // Current position in command history (-1 = not browsing)
	searchIndex  int      // Current position in search history (-1 = not browsing)
	mu           sync.RWMutex
}

// NewHistoryManager creates a new history manager using the shared database.
func NewHistoryManager(db *sqlite.DB) *HistoryManager {
	hm := &HistoryManager{
		store:        sqlite.NewLogHistoryStore(db),
		commands:     make([]string, 0),
		searches:     make([]string, 0),
		commandIndex: -1,
		searchIndex:  -1,
	}

	// Load recent history into caches
	hm.loadCaches()

	return hm
}

// loadCaches loads recent history entries from SQLite into in-memory caches.
func (hm *HistoryManager) loadCaches() {
	if hm.store == nil {
		return
	}

	// Load commands
	commands, err := hm.store.GetRecentCommands(MaxHistoryEntries)
	if err == nil {
		hm.mu.Lock()
		// Reverse order (so oldest is first, newest is last for navigation)
		hm.commands = make([]string, len(commands))
		for i, entry := range commands {
			hm.commands[len(commands)-1-i] = entry.Content
		}
		hm.mu.Unlock()
	}

	// Load searches
	searches, err := hm.store.GetRecentSearches(MaxHistoryEntries)
	if err == nil {
		hm.mu.Lock()
		// Reverse order (so oldest is first, newest is last for navigation)
		hm.searches = make([]string, len(searches))
		for i, entry := range searches {
			hm.searches[len(searches)-1-i] = entry.Content
		}
		hm.mu.Unlock()
	}
}

// AddCommand adds a command to history with shell-style deduplication.
func (hm *HistoryManager) AddCommand(command string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}

	// Persist to SQLite
	if hm.store != nil {
		if err := hm.store.AddCommand(command); err != nil {
			return
		}
	}

	// Reload cache from SQLite to stay in sync
	hm.loadCommandCache()

	// Reset navigation index
	hm.mu.Lock()
	hm.commandIndex = -1
	hm.mu.Unlock()
}

// loadCommandCache reloads only the command cache.
func (hm *HistoryManager) loadCommandCache() {
	if hm.store == nil {
		return
	}

	commands, err := hm.store.GetRecentCommands(MaxHistoryEntries)
	if err != nil {
		return
	}

	hm.mu.Lock()
	defer hm.mu.Unlock()

	hm.commands = make([]string, len(commands))
	for i, entry := range commands {
		hm.commands[len(commands)-1-i] = entry.Content
	}
}

// PreviousCommand returns the previous command in history.
// Returns empty string if at the beginning or history is empty.
func (hm *HistoryManager) PreviousCommand() string {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	if len(hm.commands) == 0 {
		return ""
	}

	if hm.commandIndex == -1 {
		// Start from the end (most recent)
		hm.commandIndex = len(hm.commands) - 1
	} else if hm.commandIndex > 0 {
		hm.commandIndex--
	}

	return hm.commands[hm.commandIndex]
}

// NextCommand returns the next command in history (more recent).
// Returns empty string if at the end or past the end.
func (hm *HistoryManager) NextCommand() string {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	if len(hm.commands) == 0 || hm.commandIndex == -1 {
		return ""
	}

	if hm.commandIndex < len(hm.commands)-1 {
		hm.commandIndex++
		return hm.commands[hm.commandIndex]
	}

	// Past the end - return empty to allow clearing input
	hm.commandIndex = -1
	return ""
}

// ResetCommandNavigation resets the command history navigation position.
func (hm *HistoryManager) ResetCommandNavigation() {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.commandIndex = -1
}

// IsBrowsingCommands returns true if currently navigating command history.
func (hm *HistoryManager) IsBrowsingCommands() bool {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return hm.commandIndex >= 0
}

// AddSearch adds a search pattern to history with shell-style deduplication.
func (hm *HistoryManager) AddSearch(pattern string) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return
	}

	// Persist to SQLite
	if hm.store != nil {
		if err := hm.store.AddSearch(pattern); err != nil {
			return
		}
	}

	// Reload cache from SQLite to stay in sync
	hm.loadSearchCache()

	// Reset navigation index
	hm.mu.Lock()
	hm.searchIndex = -1
	hm.mu.Unlock()
}

// loadSearchCache reloads only the search cache.
func (hm *HistoryManager) loadSearchCache() {
	if hm.store == nil {
		return
	}

	searches, err := hm.store.GetRecentSearches(MaxHistoryEntries)
	if err != nil {
		return
	}

	hm.mu.Lock()
	defer hm.mu.Unlock()

	hm.searches = make([]string, len(searches))
	for i, entry := range searches {
		hm.searches[len(searches)-1-i] = entry.Content
	}
}

// PreviousSearch returns the previous search pattern in history.
// Returns empty string if at the beginning or history is empty.
func (hm *HistoryManager) PreviousSearch() string {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	if len(hm.searches) == 0 {
		return ""
	}

	if hm.searchIndex == -1 {
		// Start from the end (most recent)
		hm.searchIndex = len(hm.searches) - 1
	} else if hm.searchIndex > 0 {
		hm.searchIndex--
	}

	return hm.searches[hm.searchIndex]
}

// NextSearch returns the next search pattern in history (more recent).
// Returns empty string if at the end or past the end.
func (hm *HistoryManager) NextSearch() string {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	if len(hm.searches) == 0 || hm.searchIndex == -1 {
		return ""
	}

	if hm.searchIndex < len(hm.searches)-1 {
		hm.searchIndex++
		return hm.searches[hm.searchIndex]
	}

	// Past the end - return empty to allow clearing input
	hm.searchIndex = -1
	return ""
}

// ResetSearchNavigation resets the search history navigation position.
func (hm *HistoryManager) ResetSearchNavigation() {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.searchIndex = -1
}

// IsBrowsingSearches returns true if currently navigating search history.
func (hm *HistoryManager) IsBrowsingSearches() bool {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return hm.searchIndex >= 0
}

// CommandCount returns the number of commands in history.
func (hm *HistoryManager) CommandCount() int {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return len(hm.commands)
}

// SearchCount returns the number of searches in history.
func (hm *HistoryManager) SearchCount() int {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return len(hm.searches)
}
