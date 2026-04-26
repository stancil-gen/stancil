package pagination

// OffsetPage holds a page of results with offset-based pagination metadata.
type OffsetPage[T any] struct {
	Items      []*T `json:"items"`
	Total      int  `json:"total"`
	Page       int  `json:"page"`
	Limit      int  `json:"limit"`
	TotalPages int  `json:"total_pages"`
}

// CursorPage holds a page of results with cursor-based pagination metadata.
type CursorPage[T any] struct {
	Items      []*T   `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
	Limit      int    `json:"limit"`
}

// NewOffsetPage creates an OffsetPage from query results.
func NewOffsetPage[T any](items []*T, total, page, limit int) *OffsetPage[T] {
	totalPages := 0
	if limit > 0 {
		totalPages = (total + limit - 1) / limit
	}
	return &OffsetPage[T]{
		Items:      items,
		Total:      total,
		Page:       page,
		Limit:      limit,
		TotalPages: totalPages,
	}
}

// NewCursorPage creates a CursorPage from query results.
func NewCursorPage[T any](items []*T, nextCursor string, hasMore bool, limit int) *CursorPage[T] {
	return &CursorPage[T]{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    hasMore,
		Limit:      limit,
	}
}
