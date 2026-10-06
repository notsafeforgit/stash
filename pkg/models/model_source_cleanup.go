package models

import "context"

// A cleanup intent records unfinished source-work cleanup, never permission to
// delete media, forget a post or cancel a current worker. Historical imports
// hold these records until current references and retention can be reviewed.
type SourceCleanupIntent struct {
	UUID               string `json:"uuid" db:"uuid"`
	CollectionUUID     string `json:"collection_uuid" db:"collection_uuid"`
	CollectionRevision int    `json:"collection_revision" db:"collection_revision"`
	Kind               string `json:"kind" db:"kind"`
	State              string `json:"state" db:"state"`
	ReferenceNamespace string `json:"reference_namespace" db:"reference_namespace"`
	ReferenceValue     string `json:"reference_value" db:"reference_value"`
	RequestedAt        string `json:"requested_at" db:"requested_at"`
	Origin             string `json:"origin" db:"origin"`
	RecordedAt         string `json:"recorded_at" db:"recorded_at"`
}

type SourceCleanupIntentReader interface {
	Find(context.Context, string) (*SourceCleanupIntent, error)
	CollectionIntents(context.Context, string, string, int) ([]SourceCleanupIntent, error)
}
