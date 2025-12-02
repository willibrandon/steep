package sqleditor

import (
	"testing"
)

// BenchmarkRenderResultsTable benchmarks table rendering with various sizes.
func BenchmarkRenderResultsTable(b *testing.B) {
	// Create a view with mock data
	view := &SQLEditorView{
		width:  120,
		height: 40,
		focus:  FocusResults,
	}

	// Generate test data: 100 rows, 20 columns
	cols := make([]Column, 20)
	for i := range cols {
		cols[i] = Column{
			Name:     "column_name_" + string(rune('a'+i)),
			TypeOID:  25, // text
			TypeName: "text",
		}
	}

	rows := make([][]any, 100)
	for i := range rows {
		row := make([]any, 20)
		for j := range row {
			row[j] = "value_data_here_0123456789"
		}
		rows[i] = row
	}

	view.results = &ResultSet{
		Columns:     cols,
		Rows:        FormatResultSet(rows),
		RawRows:     rows,
		TotalRows:   len(rows),
		CurrentPage: 1,
		PageSize:    DefaultPageSize,
		ExecutionMs: 50,
		SortColumn:  -1,
		SortAsc:     true,
	}

	// Pre-calculate column widths (as done in production)
	view.results.CalculateColWidths(32)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = view.renderResultsTable()
	}
}

// BenchmarkRenderResults benchmarks the full results rendering.
func BenchmarkRenderResults(b *testing.B) {
	view := &SQLEditorView{
		width:  120,
		height: 40,
		focus:  FocusResults,
	}

	// Generate test data: 100 rows, 20 columns
	cols := make([]Column, 20)
	for i := range cols {
		cols[i] = Column{
			Name:     "column_name_" + string(rune('a'+i)),
			TypeOID:  25, // text
			TypeName: "text",
		}
	}

	rows := make([][]any, 100)
	for i := range rows {
		row := make([]any, 20)
		for j := range row {
			row[j] = "value_data_here_0123456789"
		}
		rows[i] = row
	}

	view.results = &ResultSet{
		Columns:     cols,
		Rows:        FormatResultSet(rows),
		RawRows:     rows,
		TotalRows:   len(rows),
		CurrentPage: 1,
		PageSize:    DefaultPageSize,
		ExecutionMs: 50,
		SortColumn:  -1,
		SortAsc:     true,
	}

	// Pre-calculate column widths (as done in production)
	view.results.CalculateColWidths(32)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = view.renderResults()
	}
}

// BenchmarkRenderResultsLargeDataset benchmarks with 1000 rows, 50 columns.
func BenchmarkRenderResultsLargeDataset(b *testing.B) {
	view := &SQLEditorView{
		width:  200,
		height: 50,
		focus:  FocusResults,
	}

	// Generate large test data: 1000 rows, 50 columns
	cols := make([]Column, 50)
	for i := range cols {
		cols[i] = Column{
			Name:     "column_name_" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			TypeOID:  25,
			TypeName: "text",
		}
	}

	rows := make([][]any, 1000)
	for i := range rows {
		row := make([]any, 50)
		for j := range row {
			row[j] = "sample_value_for_testing_12345"
		}
		rows[i] = row
	}

	view.results = &ResultSet{
		Columns:     cols,
		Rows:        FormatResultSet(rows),
		RawRows:     rows,
		TotalRows:   len(rows),
		CurrentPage: 1,
		PageSize:    DefaultPageSize,
		ExecutionMs: 100,
		SortColumn:  -1,
		SortAsc:     true,
	}

	// Pre-calculate column widths
	view.results.CalculateColWidths(32)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = view.renderResults()
	}
}
