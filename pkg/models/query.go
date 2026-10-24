package models

type QueryOptions struct {
	FindFilter *FindFilterType
	Count      bool
}

type QueryResult[T comparable] struct {
	IDs   []T
	Count int
}

// ListQueryOptions separates page retrieval from an independently requested total.
type ListQueryOptions struct {
	QueryOptions
	SkipItems bool
}
