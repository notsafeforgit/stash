package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// A migration retains the effective legacy values and an explicit disposition
// for every value. It publishes a native policy, never a legacy runtime reader.
type MetadataPolicyImportDisposition struct {
	Action string `json:"action"` // mapped, replaced, retired, review
	Reason string `json:"reason"`
}

type MetadataPolicyImportDocument struct {
	Format        string                     `json:"format"`
	Version       int                        `json:"version"`
	PluginVersion string                     `json:"plugin_version"`
	CapturedAt    string                     `json:"captured_at"`
	SourceFiles   map[string]string          `json:"source_files"` // logical filename -> SHA-256
	Values        map[string]json.RawMessage `json:"values"`       // settings/name or folder/name
}

type MetadataPolicyImportDocumentRef struct {
	SourceUUID string `json:"source_uuid"`
	HeadUUID   string `json:"head_uuid"`
}

type MetadataPolicyImportInput struct {
	UUID          string                                     `json:"uuid"`
	Policy        MetadataPolicyInput                        `json:"policy"`
	Document      MetadataPolicyImportDocument               `json:"document"`
	Dispositions  map[string]MetadataPolicyImportDisposition `json:"dispositions"`
	FolderSources []MetadataPolicyImportDocumentRef          `json:"folder_sources"`
}

type MetadataPolicyImportPlan struct {
	UUID               string                            `json:"uuid"`
	InputSHA256        string                            `json:"input_sha256"`
	Collection         *SourceCollection                 `json:"collection"`
	PreviousPolicy     *MetadataPolicy                   `json:"previous_policy"`
	Definition         MetadataPolicyDefinition          `json:"definition"`
	ReferenceRevisions map[string]int                    `json:"reference_revisions"`
	FolderSources      []MetadataPolicyImportDocumentRef `json:"folder_sources"`
	ReviewKeys         []string                          `json:"review_keys"`
	PlanSHA256         string                            `json:"plan_sha256"`
}

type MetadataPolicyImport struct {
	MetadataPolicyImportPlan
	PolicyRevision int       `json:"policy_revision"`
	CreatedAt      time.Time `json:"created_at"`
}

type MetadataPolicyImportDetails struct {
	MetadataPolicyImport
	Binding MetadataPolicyImportInput `json:"binding"`
}

var (
	ErrMetadataPolicyImportInvalid  = errors.New("invalid metadata policy migration")
	ErrMetadataPolicyImportConflict = errors.New("metadata policy migration or reviewed state changed")
)

type MetadataPolicyImportReaderWriter interface {
	Preview(context.Context, MetadataPolicyImportInput, time.Time) (*MetadataPolicyImportPlan, error)
	Apply(context.Context, MetadataPolicyImportInput, string, time.Time) (*MetadataPolicyImport, error)
	Find(context.Context, string) (*MetadataPolicyImportDetails, error)
	List(context.Context, string, string, int) ([]MetadataPolicyImport, error)
}
