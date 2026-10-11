package models

import "context"

// A subscription expresses monitoring intent. It contains no downloader
// configuration, website credentials, scheduler, or depicted-performer claim.
type SourceSubscription struct {
	Collection *SourceCollection `json:"collection"`
	Revision   int               `json:"revision"`
	Enabled    bool              `json:"enabled"`
}

type SourceSubscriptionInput struct {
	ExpectedRevision int  `json:"expected_revision"`
	Enabled          bool `json:"enabled"`
}

// Bind an existing external scheduler's queued run without replaying downloads
// or changing its original request, completed windows, or retry progress.
type SourceRunSubscriptionInput struct {
	ExpectedRunRevision  int64 `json:"expected_run_revision"`
	SubscriptionRevision int   `json:"subscription_revision"`
}

type SourceSubscriptionFilter struct {
	CollectionUUID  string
	AccountUUID     string
	PerformerUUID   string
	RootUUID        string
	Namespace       string
	NamespacePrefix string
	EnabledOnly     bool
	After           string
	Offset          int
	Limit           int
	// Non-nil restricts producer reads to these explicitly granted collections.
	AllowedCollections []string
}

type SourceSubscriptionPage struct {
	Items []SourceSubscription `json:"items"`
	Total int                  `json:"total"`
}

type SourceSubscriptionReaderWriter interface {
	Find(context.Context, string) (*SourceSubscription, error)
	List(context.Context, SourceSubscriptionFilter) (*SourceSubscriptionPage, error)
	Put(context.Context, string, SourceSubscriptionInput) (*SourceSubscription, error)
	StopPerformer(context.Context, string) (int, error)
	BindRun(context.Context, string, SourceRunSubscriptionInput) error
}
