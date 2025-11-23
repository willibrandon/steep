// Package deadviz provides PostgreSQL deadlock visualization.
// Inspired by gocmdpev for EXPLAIN plan visualization.
package deadviz

import (
	"fmt"
	"io"
	"strings"

	"github.com/fatih/color"
	"github.com/willibrandon/steep/internal/storage/sqlite"
)

// Color formatters matching pev style
var (
	prefixFormat   = color.New(color.FgHiBlack).SprintFunc()
	tagFormat      = color.New(color.FgWhite, color.BgRed).SprintFunc()
	mutedFormat    = color.New(color.FgHiBlack).SprintFunc()
	boldFormat     = color.New(color.FgHiWhite).SprintFunc()
	goodFormat     = color.New(color.FgGreen).SprintFunc()
	warningFormat  = color.New(color.FgHiYellow).SprintFunc()
	criticalFormat = color.New(color.FgHiRed).SprintFunc()
	outputFormat   = color.New(color.FgCyan).SprintFunc()
	accentFormat   = color.New(color.FgHiMagenta).SprintFunc()
)

// Backend represents a process involved in a deadlock with analysis tags.
type Backend struct {
	PID             int
	Username        string
	ApplicationName string
	ClientAddr      string
	LockType        string
	LockMode        string
	RelationName    string
	Query           string
	BlockedByPID    *int

	// Analysis tags
	IsVictim     bool
	IsRoot       bool
	LongestWait  bool
	BlockedCount int // number of processes this one blocks
}

// Edge represents a wait-for relationship.
type Edge struct {
	FromPID int // waiter
	ToPID   int // holder
}

// WaitForGraph represents the deadlock wait-for graph.
type WaitForGraph struct {
	Backends     map[int]*Backend
	Edges        []Edge
	Cycles       [][]int
	sqlFormatter SQLFormatter
}

// SQLFormatter is a function that formats SQL queries (e.g., with pgFormatter and syntax highlighting).
type SQLFormatter func(sql string) string

// BuildWaitForGraph constructs a wait-for graph from a deadlock event.
func BuildWaitForGraph(event *sqlite.DeadlockEvent) *WaitForGraph {
	g := &WaitForGraph{
		Backends: make(map[int]*Backend),
	}

	// Build backends map
	for _, proc := range event.Processes {
		b := &Backend{
			PID:             proc.PID,
			Username:        proc.Username,
			ApplicationName: proc.ApplicationName,
			ClientAddr:      proc.ClientAddr,
			LockType:        proc.LockType,
			LockMode:        proc.LockMode,
			RelationName:    proc.RelationName,
			Query:           proc.Query,
			BlockedByPID:    proc.BlockedByPID,
		}

		// Mark victim
		if event.ResolvedByPID != nil && proc.PID == *event.ResolvedByPID {
			b.IsVictim = true
		}

		g.Backends[proc.PID] = b
	}

	// Build edges (waiter -> holder)
	for _, proc := range event.Processes {
		if proc.BlockedByPID != nil {
			g.Edges = append(g.Edges, Edge{
				FromPID: proc.PID,
				ToPID:   *proc.BlockedByPID,
			})
		}
	}

	// Analyze graph
	g.analyzeGraph()

	return g
}

// analyzeGraph performs analysis to tag backends.
func (g *WaitForGraph) analyzeGraph() {
	// Count how many each backend blocks
	blockedBy := make(map[int]int)
	for _, e := range g.Edges {
		blockedBy[e.ToPID]++
	}

	// Find root (blocks others but isn't blocked, or blocks the most)
	maxBlocked := 0
	var rootPID int
	for pid, b := range g.Backends {
		b.BlockedCount = blockedBy[pid]
		if b.BlockedCount > maxBlocked {
			maxBlocked = b.BlockedCount
			rootPID = pid
		}
	}
	if root, ok := g.Backends[rootPID]; ok && maxBlocked > 0 {
		root.IsRoot = true
	}

	// Find cycles using DFS
	g.Cycles = g.findCycles()

	// Mark longest wait (for now, mark first in cycle that isn't victim)
	if len(g.Cycles) > 0 && len(g.Cycles[0]) > 0 {
		for _, pid := range g.Cycles[0] {
			if b, ok := g.Backends[pid]; ok && !b.IsVictim {
				b.LongestWait = true
				break
			}
		}
	}
}

// findCycles finds cycles in the wait-for graph using DFS.
func (g *WaitForGraph) findCycles() [][]int {
	// Build adjacency list
	adj := make(map[int][]int)
	for _, e := range g.Edges {
		adj[e.FromPID] = append(adj[e.FromPID], e.ToPID)
	}

	var cycles [][]int
	visited := make(map[int]bool)
	recStack := make(map[int]bool)
	parent := make(map[int]int)

	var dfs func(node int, path []int)
	dfs = func(node int, path []int) {
		visited[node] = true
		recStack[node] = true
		path = append(path, node)

		for _, next := range adj[node] {
			if !visited[next] {
				parent[next] = node
				dfs(next, path)
			} else if recStack[next] {
				// Found cycle - extract it
				cycle := []int{}
				for i := len(path) - 1; i >= 0; i-- {
					cycle = append([]int{path[i]}, cycle...)
					if path[i] == next {
						break
					}
				}
				if len(cycle) > 0 {
					cycles = append(cycles, cycle)
				}
			}
		}

		recStack[node] = false
	}

	for pid := range g.Backends {
		if !visited[pid] {
			dfs(pid, []int{})
		}
	}

	return cycles
}

// Visualize renders a deadlock event visualization to the writer.
// If sqlFormatter is nil, queries are displayed as-is.
func Visualize(w io.Writer, event *sqlite.DeadlockEvent, width uint, sqlFormatter SQLFormatter) error {
	if event == nil || len(event.Processes) == 0 {
		return nil
	}

	g := BuildWaitForGraph(event)
	g.sqlFormatter = sqlFormatter

	// Header
	fmt.Fprintf(w, "%s\n", boldFormat(fmt.Sprintf("Deadlock Event #%d", event.ID)))
	fmt.Fprintf(w, "%s\n", strings.Repeat("─", 60))
	fmt.Fprintln(w)

	// Summary
	fmt.Fprintf(w, "%s %s\n", mutedFormat("Detected:"), event.DetectedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "%s %s\n", mutedFormat("Database:"), event.DatabaseName)
	if event.ResolvedByPID != nil {
		fmt.Fprintf(w, "%s PID %d %s\n", mutedFormat("Resolved by:"), *event.ResolvedByPID, tagFormat(" victim "))
	}
	fmt.Fprintln(w)

	// Cycle diagram
	fmt.Fprintf(w, "%s\n", boldFormat(fmt.Sprintf("Wait-For Cycle (%d backends)", len(event.Processes))))
	fmt.Fprintf(w, "%s\n", strings.Repeat("─", 60))
	fmt.Fprintln(w)

	if len(event.Processes) == 2 {
		writeTwoNodeCycle(w, g)
	} else {
		writeNNodeCycle(w, g)
	}

	fmt.Fprintln(w)

	// Per-backend details
	fmt.Fprintf(w, "%s\n", boldFormat("Backend Details"))
	fmt.Fprintf(w, "%s\n", strings.Repeat("─", 60))
	fmt.Fprintf(w, "%s\n", prefixFormat("┬"))

	pids := make([]int, 0, len(g.Backends))
	for pid := range g.Backends {
		pids = append(pids, pid)
	}
	// Sort by PID for consistent output
	for i := 0; i < len(pids)-1; i++ {
		for j := i + 1; j < len(pids); j++ {
			if pids[i] > pids[j] {
				pids[i], pids[j] = pids[j], pids[i]
			}
		}
	}

	for i, pid := range pids {
		b := g.Backends[pid]
		last := i == len(pids)-1
		writeBackend(w, b, width, last, g.sqlFormatter)
	}

	// Analysis section
	fmt.Fprintln(w)
	writeAnalysis(w, g)

	return nil
}

// writeTwoNodeCycle renders a horizontal cycle diagram for 2 backends.
func writeTwoNodeCycle(w io.Writer, g *WaitForGraph) {
	if len(g.Backends) != 2 {
		return
	}

	// Get the two PIDs
	var pids []int
	for pid := range g.Backends {
		pids = append(pids, pid)
	}
	if pids[0] > pids[1] {
		pids[0], pids[1] = pids[1], pids[0]
	}

	b1 := g.Backends[pids[0]]
	b2 := g.Backends[pids[1]]

	// Render horizontal diagram with longer relation names
	fmt.Fprintf(w, "  %s ────────────────────────────────────────▶ %s\n",
		boldFormat(fmt.Sprintf("PID %d", b1.PID)),
		boldFormat(fmt.Sprintf("PID %d", b2.PID)))
	fmt.Fprintf(w, "  %s                   %s   %s\n",
		getTags(b1),
		mutedFormat("waits for"),
		getTags(b2))
	fmt.Fprintf(w, "  %s ◀──────────────────── %s\n",
		outputFormat(truncate(b1.RelationName, 20)),
		outputFormat(truncate(b2.RelationName, 20)))
	fmt.Fprintf(w, "                %s\n", mutedFormat("(deadlock)"))
}

// writeNNodeCycle renders a vertical cycle diagram for N backends.
func writeNNodeCycle(w io.Writer, g *WaitForGraph) {
	if len(g.Cycles) == 0 {
		// No cycles detected, show linear chain
		for pid, b := range g.Backends {
			joint := "├"
			if b.BlockedByPID == nil {
				joint = "└"
			}
			fmt.Fprintf(w, "%s %s %s\n",
				prefixFormat(joint+"─▶"),
				boldFormat(fmt.Sprintf("PID %d", pid)),
				getTags(b))

			if b.BlockedByPID != nil {
				fmt.Fprintf(w, "%s   %s %s on %s held by PID %d\n",
					prefixFormat("│"),
					mutedFormat("waits for"),
					b.LockMode,
					outputFormat(b.RelationName),
					*b.BlockedByPID)
			}
		}
		return
	}

	// Render first cycle with visual cycle back to start
	cycle := g.Cycles[0]
	for i, pid := range cycle {
		b := g.Backends[pid]
		if b == nil {
			continue
		}

		isLast := i == len(cycle)-1

		// Use box drawing to show cycle visually
		if i == 0 {
			fmt.Fprintf(w, "%s %s %s\n",
				prefixFormat("┌─▶"),
				boldFormat(fmt.Sprintf("PID %d", pid)),
				getTags(b))
		} else if isLast {
			fmt.Fprintf(w, "%s %s %s\n",
				prefixFormat("│ └▶"),
				boldFormat(fmt.Sprintf("PID %d", pid)),
				getTags(b))
		} else {
			fmt.Fprintf(w, "%s %s %s\n",
				prefixFormat("│ ├▶"),
				boldFormat(fmt.Sprintf("PID %d", pid)),
				getTags(b))
		}

		if b.BlockedByPID != nil {
			holderPID := *b.BlockedByPID
			fmt.Fprintf(w, "%s   %s %s on %s held by PID %d\n",
				prefixFormat("│"),
				mutedFormat("waits for"),
				warningFormat(b.LockMode),
				outputFormat(b.RelationName),
				holderPID)
		}

		if !isLast {
			fmt.Fprintf(w, "%s\n", prefixFormat("│"))
		}
	}

	// Close the cycle visually back to start
	if len(cycle) > 0 {
		fmt.Fprintf(w, "%s\n", prefixFormat("│"))
		fmt.Fprintf(w, "%s %s\n", prefixFormat("└─────────────────────────────────────────┘"), mutedFormat("(deadlock cycle)"))
	}
}

// writeBackend renders a single backend in tree style.
func writeBackend(w io.Writer, b *Backend, width uint, last bool, sqlFormatter SQLFormatter) {
	joint := "├"
	prefix := "│"
	if last {
		joint = "└"
		prefix = " "
	}

	// Backend header with tags
	fmt.Fprintf(w, "%s %s %s\n",
		prefixFormat(joint+"─⌠"),
		boldFormat(fmt.Sprintf("PID %d", b.PID)),
		getTags(b))

	// Backend details
	currentPrefix := prefix + " │ "

	if b.Username != "" {
		fmt.Fprintf(w, "%s%s %s\n", prefixFormat(currentPrefix), mutedFormat("User:"), b.Username)
	}
	if b.ApplicationName != "" {
		fmt.Fprintf(w, "%s%s %s\n", prefixFormat(currentPrefix), mutedFormat("Application:"), b.ApplicationName)
	}
	if b.ClientAddr != "" {
		fmt.Fprintf(w, "%s%s %s\n", prefixFormat(currentPrefix), mutedFormat("Client:"), b.ClientAddr)
	}
	if b.LockType != "" {
		fmt.Fprintf(w, "%s%s %s\n", prefixFormat(currentPrefix), mutedFormat("Lock Type:"), b.LockType)
	}
	if b.LockMode != "" {
		fmt.Fprintf(w, "%s%s %s\n", prefixFormat(currentPrefix), mutedFormat("Lock Mode:"), warningFormat(b.LockMode))
	}
	if b.RelationName != "" {
		fmt.Fprintf(w, "%s%s %s\n", prefixFormat(currentPrefix), mutedFormat("Relation:"), outputFormat(b.RelationName))
	}
	if b.BlockedByPID != nil {
		fmt.Fprintf(w, "%s%s PID %d\n", prefixFormat(currentPrefix), criticalFormat("Blocked by:"), *b.BlockedByPID)
	}

	// Query
	if b.Query != "" {
		fmt.Fprintf(w, "%s%s\n", prefixFormat(currentPrefix), mutedFormat("Query:"))

		// Format and display query
		queryToDisplay := b.Query
		if sqlFormatter != nil {
			queryToDisplay = sqlFormatter(b.Query)
		}

		for _, line := range strings.Split(queryToDisplay, "\n") {
			fmt.Fprintf(w, "%s  %s\n", prefixFormat(currentPrefix), line)
		}
	}

	fmt.Fprintf(w, "%s\n", prefixFormat(prefix))
}

// writeAnalysis renders the analysis section with pattern detection and suggestions.
func writeAnalysis(w io.Writer, g *WaitForGraph) {
	fmt.Fprintf(w, "%s\n", boldFormat("Analysis"))
	fmt.Fprintf(w, "%s\n", strings.Repeat("─", 60))
	fmt.Fprintln(w)

	// Detect patterns
	patterns := detectPatterns(g)

	if len(patterns) > 0 {
		fmt.Fprintf(w, "%s\n", accentFormat("Detected Patterns:"))
		for _, p := range patterns {
			fmt.Fprintf(w, "  • %s\n", p)
		}
		fmt.Fprintln(w)
	}

	// Suggestions
	suggestions := generateSuggestions(g)

	if len(suggestions) > 0 {
		fmt.Fprintf(w, "%s\n", goodFormat("Suggested Fixes:"))
		for _, s := range suggestions {
			fmt.Fprintf(w, "  • %s\n", s)
		}
	}
}

// detectPatterns analyzes the deadlock for common patterns.
func detectPatterns(g *WaitForGraph) []string {
	var patterns []string

	// Check if all processes are on the same table
	tables := make(map[string]int)
	for _, b := range g.Backends {
		if b.RelationName != "" {
			tables[b.RelationName]++
		}
	}

	if len(tables) == 1 {
		for table := range tables {
			patterns = append(patterns, fmt.Sprintf("All backends contending on same table: %s", outputFormat(table)))
		}

		// Likely row-level lock contention with inconsistent order
		if len(g.Backends) == 2 {
			patterns = append(patterns, "Classic two-process deadlock - likely inconsistent row access order")
		}
	}

	// Check lock modes
	modes := make(map[string]int)
	for _, b := range g.Backends {
		if b.LockMode != "" {
			modes[b.LockMode]++
		}
	}

	if len(modes) == 1 {
		for mode := range modes {
			if mode == "ShareLock" || mode == "ExclusiveLock" {
				patterns = append(patterns, fmt.Sprintf("All backends using %s mode", warningFormat(mode)))
			}
		}
	}

	// Check for UPDATE statements
	updateCount := 0
	for _, b := range g.Backends {
		if strings.Contains(strings.ToUpper(b.Query), "UPDATE") {
			updateCount++
		}
	}
	if updateCount == len(g.Backends) {
		patterns = append(patterns, "All backends executing UPDATE statements")
	}

	return patterns
}

// generateSuggestions generates fix suggestions based on the deadlock.
func generateSuggestions(g *WaitForGraph) []string {
	var suggestions []string

	// Check for row access order issues
	tables := make(map[string]int)
	for _, b := range g.Backends {
		if b.RelationName != "" {
			tables[b.RelationName]++
		}
	}

	if len(tables) == 1 && len(g.Backends) == 2 {
		suggestions = append(suggestions,
			"Ensure transactions access rows in consistent order (e.g., by primary key ASC)")
	}

	// General suggestions
	suggestions = append(suggestions,
		"Keep transactions as short as possible",
		"Consider using NOWAIT or SKIP LOCKED for advisory locking",
	)

	// If multiple tables involved
	if len(tables) > 1 {
		suggestions = append(suggestions,
			"Access tables in consistent order across all transactions")
	}

	return suggestions
}

// Helper functions

func getTags(b *Backend) string {
	var tags []string

	if b.IsVictim {
		tags = append(tags, tagFormat(" victim "))
	}
	if b.IsRoot {
		tags = append(tags, warningFormat(" root "))
	}
	if b.LongestWait {
		tags = append(tags, mutedFormat(" longest wait "))
	}

	return strings.Join(tags, " ")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
