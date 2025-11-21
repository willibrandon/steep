package models

// ActivityFilter defines user-applied filters for the activity table.
type ActivityFilter struct {
	StateFilter      string `json:"state_filter"`
	DatabaseFilter   string `json:"database_filter"`
	QueryFilter      string `json:"query_filter"`
	ShowAllDatabases bool   `json:"show_all_databases"`
}

// IsEmpty returns true if no filters are applied.
func (f *ActivityFilter) IsEmpty() bool {
	return f.StateFilter == "" && f.DatabaseFilter == "" && f.QueryFilter == ""
}

// Clear resets all filters to empty values.
func (f *ActivityFilter) Clear() {
	f.StateFilter = ""
	f.DatabaseFilter = ""
	f.QueryFilter = ""
}

// Pagination holds state for paginated results.
type Pagination struct {
	Limit      int  `json:"limit"`
	Offset     int  `json:"offset"`
	TotalCount int  `json:"total_count"`
	HasMore    bool `json:"has_more"`
}

// NewPagination creates a new Pagination with default limit of 500.
func NewPagination() *Pagination {
	return &Pagination{
		Limit:  500,
		Offset: 0,
	}
}

// CurrentPage returns the current page number (1-based).
func (p *Pagination) CurrentPage() int {
	if p.Limit == 0 {
		return 1
	}
	return (p.Offset / p.Limit) + 1
}

// TotalPages returns the total number of pages.
func (p *Pagination) TotalPages() int {
	if p.Limit == 0 || p.TotalCount == 0 {
		return 1
	}
	return (p.TotalCount + p.Limit - 1) / p.Limit
}

// NextPage advances to the next page if available.
func (p *Pagination) NextPage() {
	if p.HasMore {
		p.Offset += p.Limit
	}
}

// PrevPage moves to the previous page if available.
func (p *Pagination) PrevPage() {
	if p.Offset >= p.Limit {
		p.Offset -= p.Limit
	} else {
		p.Offset = 0
	}
}

// Reset moves to the first page.
func (p *Pagination) Reset() {
	p.Offset = 0
}

// Update recalculates HasMore based on total count.
func (p *Pagination) Update(totalCount int) {
	p.TotalCount = totalCount
	p.HasMore = p.Offset+p.Limit < totalCount
}
