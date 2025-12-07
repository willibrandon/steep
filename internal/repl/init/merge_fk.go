package init

import (
	"context"
	"fmt"
	"sort"
)

// =============================================================================
// FK Ordering (T073b)
// =============================================================================

// GetFKDependencies retrieves foreign key dependencies for the given tables.
func (m *Merger) GetFKDependencies(ctx context.Context, tables []MergeTableInfo) ([]FKDependency, error) {
	query := `
		SELECT
			tc.table_schema as child_schema,
			tc.table_name as child_table,
			ccu.table_schema as parent_schema,
			ccu.table_name as parent_table
		FROM information_schema.table_constraints tc
		JOIN information_schema.constraint_column_usage ccu
			ON tc.constraint_name = ccu.constraint_name
			AND tc.constraint_schema = ccu.constraint_schema
		WHERE tc.constraint_type = 'FOREIGN KEY'
			AND tc.table_schema || '.' || tc.table_name = ANY($1)
	`

	var fullNames []string
	for _, t := range tables {
		fullNames = append(fullNames, fmt.Sprintf("%s.%s", t.Schema, t.Name))
	}

	rows, err := m.localPool.Query(ctx, query, fullNames)
	if err != nil {
		return nil, fmt.Errorf("get FK dependencies: %w", err)
	}
	defer rows.Close()

	var deps []FKDependency
	for rows.Next() {
		var dep FKDependency
		if err := rows.Scan(&dep.ChildSchema, &dep.ChildTable, &dep.ParentSchema, &dep.ParentTable); err != nil {
			return nil, err
		}
		deps = append(deps, dep)
	}

	return deps, rows.Err()
}

// TopologicalSort sorts tables in dependency order (parents before children).
// Uses Kahn's algorithm for topological sorting.
func (m *Merger) TopologicalSort(tables []MergeTableInfo, deps []FKDependency) ([]MergeTableInfo, error) {
	// Build adjacency list and in-degree map
	// Edge: parent -> child (parent must come before child)
	graph := make(map[string][]string)
	inDegree := make(map[string]int)

	// Initialize all tables
	for _, t := range tables {
		key := fmt.Sprintf("%s.%s", t.Schema, t.Name)
		if _, ok := graph[key]; !ok {
			graph[key] = []string{}
		}
		if _, ok := inDegree[key]; !ok {
			inDegree[key] = 0
		}
	}

	// Add edges from dependencies
	for _, dep := range deps {
		parent := fmt.Sprintf("%s.%s", dep.ParentSchema, dep.ParentTable)
		child := fmt.Sprintf("%s.%s", dep.ChildSchema, dep.ChildTable)

		// Only add if both tables are in our set
		if _, ok := inDegree[parent]; !ok {
			continue
		}
		if _, ok := inDegree[child]; !ok {
			continue
		}

		graph[parent] = append(graph[parent], child)
		inDegree[child]++
	}

	// Kahn's algorithm
	var queue []string
	for key, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, key)
		}
	}

	// Sort queue for deterministic output
	sort.Strings(queue)

	var sorted []string
	for len(queue) > 0 {
		// Pop from queue
		current := queue[0]
		queue = queue[1:]
		sorted = append(sorted, current)

		// Process neighbors
		neighbors := graph[current]
		sort.Strings(neighbors) // Deterministic order

		for _, neighbor := range neighbors {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
				sort.Strings(queue)
			}
		}
	}

	// Check for cycles
	if len(sorted) != len(inDegree) {
		return nil, fmt.Errorf("circular foreign key dependency detected")
	}

	// Map back to MergeTableInfo
	tableMap := make(map[string]MergeTableInfo)
	for _, t := range tables {
		key := fmt.Sprintf("%s.%s", t.Schema, t.Name)
		tableMap[key] = t
	}

	var result []MergeTableInfo
	for _, key := range sorted {
		result = append(result, tableMap[key])
	}

	return result, nil
}
