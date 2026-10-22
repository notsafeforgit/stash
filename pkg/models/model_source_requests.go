package models

import "time"

// Request permits apply to extractor HTTP attempts, never media transfers.
type SourceRequestInput struct {
	RequestUUID string `json:"request_uuid"`
	URL         string `json:"url"`
	Class       string `json:"class"`
	Operation   string `json:"operation"`
}
type SourceRequestPermit struct {
	RequestUUID string `json:"request_uuid"`
	Scope       string `json:"source_scope"`
	Ready       bool   `json:"ready"`
	WaitMS      int64  `json:"wait_ms"`
}
type SourceRequestResult struct {
	RequestUUID       string `json:"request_uuid"`
	Status            int    `json:"status"`
	RetryAfterSeconds int    `json:"retry_after_seconds"`
	BlockReason       string `json:"block_reason,omitempty"`
}
type SourceRequestMetric struct {
	Scope         string `json:"source_scope" db:"scope"`
	Class         string `json:"class" db:"class"`
	Operation     string `json:"operation" db:"operation"`
	Requests      int64  `json:"requests" db:"requests"`
	Responses     int64  `json:"responses" db:"responses"`
	RateLimited   int64  `json:"rate_limited" db:"rate_limited"`
	Errors        int64  `json:"errors" db:"errors"`
	NewPosts      int64  `json:"new_posts" db:"new_posts"`
	RepeatedPosts int64  `json:"repeated_posts" db:"repeated_posts"`
}
type SourceCheckHealth struct {
	CollectionUUID string `json:"collection_uuid" db:"collection_uuid"`
	Label          string `json:"label" db:"label"`
	URL            string `json:"url" db:"url"`
	LastSuccessMS  int64  `json:"last_success_ms" db:"last_success_ms"`
	OverdueSeconds int64  `json:"overdue_seconds" db:"overdue_seconds"`
}
type SourceRequestSummary struct {
	Since    time.Time             `json:"since"`
	Metrics  []SourceRequestMetric `json:"metrics"`
	Accounts []SourceCheckHealth   `json:"accounts"`
	Total    int                   `json:"total"`
}
