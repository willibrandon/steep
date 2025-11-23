// Package components provides reusable UI components for Steep.
package components

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/xlab/treeprint"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// RenderLockTree renders blocking chains as an ASCII tree.
// Returns an empty string if there are no blocking chains.
func RenderLockTree(chains []models.BlockingChain, width int) string {
	if len(chains) == 0 {
		return ""
	}

	tree := treeprint.New()
	tree.SetValue("Blocking Chains")

	for _, chain := range chains {
		addChainToTree(tree, chain, width)
	}

	// Style the tree output
	treeStyle := lipgloss.NewStyle().
		Foreground(styles.ColorMuted)

	return treeStyle.Render(tree.String())
}

// addChainToTree recursively adds a blocking chain to the tree.
func addChainToTree(branch treeprint.Tree, chain models.BlockingChain, width int) {
	// Format the node: PID (user) - truncated query
	nodeText := formatTreeNode(chain, width)

	// Color based on whether this node blocks others
	var styledNode string
	if len(chain.Blocked) > 0 {
		// This is a blocker (yellow)
		blockerStyle := lipgloss.NewStyle().Foreground(styles.ColorBlocking)
		styledNode = blockerStyle.Render(nodeText)
	} else {
		// This is blocked (red) - leaf node
		blockedStyle := lipgloss.NewStyle().Foreground(styles.ColorBlocked)
		styledNode = blockedStyle.Render(nodeText)
	}

	if len(chain.Blocked) > 0 {
		// Has children - create a branch
		subBranch := branch.AddBranch(styledNode)
		for _, blocked := range chain.Blocked {
			addChainToTree(subBranch, blocked, width)
		}
	} else {
		// Leaf node
		branch.AddNode(styledNode)
	}
}

// formatTreeNode formats a single tree node with PID, user, and truncated query.
func formatTreeNode(chain models.BlockingChain, width int) string {
	// Calculate available width for query (leave room for PID and user)
	// Format: "PID 12345 (user) - query..."
	pidUserLen := len(fmt.Sprintf("PID %d (%s) - ", chain.BlockerPID, chain.User))
	queryWidth := width - pidUserLen - 20 // Extra margin for tree characters
	if queryWidth < 10 {
		queryWidth = 10
	}

	query := truncateQuery(chain.Query, queryWidth)
	return fmt.Sprintf("PID %d (%s) - %s", chain.BlockerPID, chain.User, query)
}

// truncateQuery truncates a query string for display in tree nodes.
func truncateQuery(query string, maxLen int) string {
	if len(query) == 0 {
		return "(no query)"
	}
	if len(query) <= maxLen {
		return query
	}
	if maxLen <= 3 {
		return "..."
	}
	return query[:maxLen-3] + "..."
}
