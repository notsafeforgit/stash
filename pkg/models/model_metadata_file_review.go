package models

import (
	"encoding/json"
	"errors"
	"time"
)

// Names in retained catalogs are evidence, not UUIDs. A reviewed selection can
// explicitly associate a name with a differently named native entity.
type MetadataNameSelection struct {
	UUID     string `json:"uuid"`
	Revision int    `json:"revision"`
}

type MetadataFileEditInput struct {
	EntityUUID  string                           `json:"entity_uuid"`
	HistoryUUID string                           `json:"history_uuid"`
	SourceField string                           `json:"source_field"`
	MatchUUID   string                           `json:"match_uuid"`
	Selections  map[string]MetadataNameSelection `json:"selections,omitempty"`
}

type MetadataFileEditApplyInput struct {
	MetadataFileEditInput
	RequestUUID string `json:"request_uuid"`
	Digest      string `json:"digest"`
	KeepCurrent bool   `json:"keep_current,omitempty"`
}

type MetadataNameCandidate struct {
	UUID           string `json:"uuid" db:"uuid"`
	LocalID        int    `json:"local_id" db:"local_id"`
	Revision       int    `json:"revision" db:"revision"`
	Name           string `json:"name" db:"name"`
	Disambiguation string `json:"disambiguation,omitempty" db:"disambiguation"`
}

type MetadataNameMatch struct {
	Name       string                  `json:"name"`
	Candidates []MetadataNameCandidate `json:"candidates"`
	More       bool                    `json:"more"`
	Selected   *MetadataNameCandidate  `json:"selected,omitempty"`
}

type MetadataFileEditPreview struct {
	Input               MetadataFileEditInput `json:"input"`
	EntityRevision      int                   `json:"entity_revision"`
	Field               string                `json:"field"`
	CurrentValue        json.RawMessage       `json:"current_value"`
	CurrentMode         string                `json:"current_mode"`
	CurrentOrigin       string                `json:"current_origin"`
	CurrentDecisionUUID string                `json:"current_decision_uuid,omitempty"`
	Protected           bool                  `json:"protected"`
	Mode                string                `json:"mode"`
	Value               json.RawMessage       `json:"value,omitempty"`
	FileUUID            string                `json:"file_uuid"`
	Generation          int64                 `json:"generation"`
	ArchiveFileUUID     *string               `json:"archive_file_uuid,omitempty"`
	ArchiveGeneration   *int64                `json:"archive_generation,omitempty"`
	ReferenceRevisions  map[string]int        `json:"reference_revisions,omitempty"`
	Names               []MetadataNameMatch   `json:"names,omitempty"`
	Status              string                `json:"status"` // ready, unresolved_names, unsupported
	Digest              string                `json:"digest"`
}

// A receipt is independent of the current field head. Replaying its exact
// request returns the original decision even after later library edits.
type MetadataFileEditReview struct {
	RequestUUID  string                     `json:"request_uuid"`
	DecisionUUID string                     `json:"decision_uuid,omitempty"`
	KeptCurrent  bool                       `json:"kept_current,omitempty"`
	Field        string                     `json:"field"`
	Request      MetadataFileEditApplyInput `json:"request"`
	CreatedAt    time.Time                  `json:"created_at"`
}

type MetadataFileEditCandidate struct {
	HistoryUUID    string `json:"history_uuid" db:"history_uuid"`
	MatchUUID      string `json:"match_uuid" db:"match_uuid"`
	CollectionUUID string `json:"collection_uuid" db:"collection_uuid"`
	SourceTime     string `json:"source_time" db:"source_time"`
	RelativePath   string `json:"relative_path" db:"relative_path"`
	FileUUID       string `json:"file_uuid" db:"file_uuid"`
}

var (
	ErrMetadataFileReviewInvalid = errors.New("invalid historical metadata review")
	ErrMetadataFileReviewReplay  = errors.New("historical metadata review UUID has different contents")
)
