package sqleditor

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// handleSearchInput handles keyboard input during Ctrl+R search mode.
func (v *SQLEditorView) handleSearchInput(key string) tea.Cmd {
	switch key {
	case "esc":
		// Cancel search
		v.searchMode = false
		v.searchQuery = ""
		v.searchResult = nil
		return nil

	case "enter":
		// Accept current selection
		if len(v.searchResult) > 0 && v.searchIndex < len(v.searchResult) {
			v.editor.SetContent(v.searchResult[v.searchIndex].SQL)
		}
		v.searchMode = false
		v.searchQuery = ""
		v.searchResult = nil
		return nil

	case "ctrl+s", "up":
		// Navigate up visually (to newer/more recent entries)
		if v.searchIndex > 0 {
			v.searchIndex--
		}
		return nil

	case "ctrl+r", "down":
		// Navigate down visually (to older entries)
		if len(v.searchResult) > 0 && v.searchIndex < len(v.searchResult)-1 {
			v.searchIndex++
		}
		return nil

	case "backspace":
		// Delete last character from search query
		if len(v.searchQuery) > 0 {
			v.searchQuery = v.searchQuery[:len(v.searchQuery)-1]
			v.searchResult = v.history.Search(v.searchQuery)
			v.searchIndex = 0
		}
		return nil

	default:
		// Add character to search query (single printable characters only)
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			v.searchQuery += key
			v.searchResult = v.history.Search(v.searchQuery)
			v.searchIndex = 0
		}
		return nil
	}
}

// saveSnippetCmd handles the :save NAME command.
// If snippet exists, warns user to use :save! to overwrite.
func (v *SQLEditorView) saveSnippetCmd(args []string) tea.Cmd {
	if v.snippets == nil {
		v.showToast("Snippet manager not available", true)
		return nil
	}

	if len(args) == 0 {
		v.showToast("Usage: :save NAME", true)
		return nil
	}

	name := args[0]
	sql := strings.TrimSpace(v.editor.GetBuffer().Text())
	if sql == "" {
		v.showToast("No query to save", true)
		return nil
	}

	// Check if snippet exists - require :save! to overwrite
	if v.snippets.Exists(name) {
		v.showToast(fmt.Sprintf("Snippet '%s' exists. Use :save! %s to overwrite", name, name), true)
		return nil
	}

	_, err := v.snippets.Save(name, sql, "")
	if err != nil {
		v.showToast(fmt.Sprintf("Save failed: %s", err.Error()), true)
		return nil
	}

	v.showToast(fmt.Sprintf("Saved snippet '%s'", name), false)
	return nil
}

// saveSnippetForceCmd handles the :save! NAME command (force overwrite).
func (v *SQLEditorView) saveSnippetForceCmd(args []string) tea.Cmd {
	if v.snippets == nil {
		v.showToast("Snippet manager not available", true)
		return nil
	}

	if len(args) == 0 {
		v.showToast("Usage: :save! NAME", true)
		return nil
	}

	name := args[0]
	sql := strings.TrimSpace(v.editor.GetBuffer().Text())
	if sql == "" {
		v.showToast("No query to save", true)
		return nil
	}

	overwritten, err := v.snippets.Save(name, sql, "")
	if err != nil {
		v.showToast(fmt.Sprintf("Save failed: %s", err.Error()), true)
		return nil
	}

	if overwritten {
		v.showToast(fmt.Sprintf("Updated snippet '%s'", name), false)
	} else {
		v.showToast(fmt.Sprintf("Saved snippet '%s'", name), false)
	}
	return nil
}

// loadSnippetCmd handles the :load NAME command.
func (v *SQLEditorView) loadSnippetCmd(args []string) tea.Cmd {
	if v.snippets == nil {
		v.showToast("Snippet manager not available", true)
		return nil
	}

	if len(args) == 0 {
		v.showToast("Usage: :load NAME", true)
		return nil
	}

	name := args[0]
	snippet, err := v.snippets.Load(name)
	if err != nil {
		v.showToast(err.Error(), true)
		return nil
	}

	v.editor.SetContent(snippet.SQL)
	v.editor.SetCursorPosition(0, 0)
	v.showToast(fmt.Sprintf("Loaded snippet '%s'", name), false)
	return nil
}

// deleteSnippetCmd handles the :delete NAME command for snippets.
func (v *SQLEditorView) deleteSnippetCmd(args []string) tea.Cmd {
	if v.snippets == nil {
		v.showToast("Snippet manager not available", true)
		return nil
	}

	if len(args) == 0 {
		v.showToast("Usage: :delete NAME", true)
		return nil
	}

	name := args[0]
	if err := v.snippets.Delete(name); err != nil {
		v.showToast(err.Error(), true)
		return nil
	}

	v.showToast(fmt.Sprintf("Deleted snippet '%s'", name), false)
	return nil
}

// exportCmd handles the :export FORMAT FILENAME command.
// FORMAT can be 'csv' or 'json'.
// Examples:
//
//	:export csv results.csv
//	:export json ~/data/output.json
func (v *SQLEditorView) exportCmd(args []string) tea.Cmd {
	if len(args) < 2 {
		v.showToast("Usage: :export csv|json FILENAME", true)
		return nil
	}

	format := strings.ToLower(args[0])
	filename := strings.Join(args[1:], " ") // Support filenames with spaces

	if v.results == nil || len(v.results.Rows) == 0 {
		v.showToast("No results to export", true)
		return nil
	}

	var result *ExportResult
	switch format {
	case "csv":
		result = ExportCSV(v.results, filename)
	case "json":
		result = ExportJSON(v.results, filename)
	default:
		v.showToast(fmt.Sprintf("Unknown format '%s'. Use 'csv' or 'json'", format), true)
		return nil
	}

	if result.Error != nil {
		v.showToast(fmt.Sprintf("Export failed: %s", result.Error.Error()), true)
		return nil
	}

	v.showToast(FormatExportSuccess(result), false)
	return nil
}

// openSnippetBrowser opens the snippet browser overlay.
func (v *SQLEditorView) openSnippetBrowser() {
	if v.snippets == nil {
		v.showToast("Snippet manager not available", true)
		return
	}

	v.mode = ModeSnippetBrowser
	v.snippetBrowsing = true
	v.snippetSearchQuery = ""
	v.snippetList = v.snippets.List()
	v.snippetIndex = 0
}

// closeSnippetBrowser closes the snippet browser overlay.
func (v *SQLEditorView) closeSnippetBrowser() {
	v.mode = ModeNormal
	v.snippetBrowsing = false
	v.snippetSearchQuery = ""
	v.snippetList = nil
	v.snippetIndex = 0
}

// handleSnippetBrowserInput handles keyboard input in snippet browser mode.
func (v *SQLEditorView) handleSnippetBrowserInput(key string) tea.Cmd {
	switch key {
	case "esc", "ctrl+o":
		v.closeSnippetBrowser()
		return nil

	case "enter":
		if len(v.snippetList) > 0 && v.snippetIndex < len(v.snippetList) {
			snippet := v.snippetList[v.snippetIndex]
			v.editor.SetContent(snippet.SQL)
			v.editor.SetCursorPosition(0, 0)
			v.closeSnippetBrowser()
			v.showToast(fmt.Sprintf("Loaded snippet '%s'", snippet.Name), false)
		}
		return nil

	case "up", "k":
		if v.snippetIndex > 0 {
			v.snippetIndex--
		}
		return nil

	case "down", "j":
		if v.snippetIndex < len(v.snippetList)-1 {
			v.snippetIndex++
		}
		return nil

	case "g":
		v.snippetIndex = 0
		return nil

	case "G":
		if len(v.snippetList) > 0 {
			v.snippetIndex = len(v.snippetList) - 1
		}
		return nil

	case "d", "delete":
		// Delete current snippet
		if len(v.snippetList) > 0 && v.snippetIndex < len(v.snippetList) {
			name := v.snippetList[v.snippetIndex].Name
			if err := v.snippets.Delete(name); err == nil {
				v.showToast(fmt.Sprintf("Deleted snippet '%s'", name), false)
				v.snippetList = v.snippets.Search(v.snippetSearchQuery)
				if v.snippetIndex >= len(v.snippetList) && v.snippetIndex > 0 {
					v.snippetIndex--
				}
			}
		}
		return nil

	case "backspace":
		if len(v.snippetSearchQuery) > 0 {
			v.snippetSearchQuery = v.snippetSearchQuery[:len(v.snippetSearchQuery)-1]
			v.snippetList = v.snippets.Search(v.snippetSearchQuery)
			v.snippetIndex = 0
		}
		return nil

	default:
		// Add to search query if printable character
		if len(key) == 1 && key[0] >= 32 && key[0] <= 126 {
			v.snippetSearchQuery += key
			v.snippetList = v.snippets.Search(v.snippetSearchQuery)
			v.snippetIndex = 0
		}
		return nil
	}
}
