package models

import (
	"encoding/json"
	"time"
)

// AccountProfileInput retains standalone profile evidence without inventing a
// source post. The ingestion receipt and URL changes commit with this record.
type AccountProfileInput struct {
	UUID               string
	ProducerUUID       string
	EventUUID          string
	CollectionUUID     string
	CollectionRevision int
	ObservedAt         time.Time
	ExtractorVersion   string
	Source             json.RawMessage
}

type AccountProfileResult struct {
	Status        string `json:"status"`
	ProfileUUID   string `json:"profile_uuid"`
	AccountUUID   string `json:"account_uuid,omitempty"`
	Review        string `json:"review,omitempty"`
	MediaIngested bool   `json:"media_ingested"`
}
