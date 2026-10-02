package models

// CollectionPostMembership is evidence that a source post belonged to a
// collection. It neither identifies the publisher nor assigns performers.
// It can exist without a scrape capture, downloadable file or library entity.
type CollectionPostMembership struct {
	SourcePostEvidence
	CollectionUUID     string `json:"collection_uuid"`
	CollectionRevision int    `json:"collection_revision"`
}
