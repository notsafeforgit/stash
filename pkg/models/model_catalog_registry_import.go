package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// The registry snapshot complements an already retained performer snapshot from
// the same source database and capture time. It contains the other six families.
type CatalogRegistryImportInput struct {
	UUID               string          `json:"uuid"`
	SourceUUID         string          `json:"source_uuid"`
	IdentityImportUUID string          `json:"identity_import_uuid"`
	Document           json.RawMessage `json:"document"`
}

type CatalogRegistryAccountAction struct {
	Key         string   `json:"key"`
	AccountUUID string   `json:"account_uuid"`
	Namespace   string   `json:"namespace"`
	Label       string   `json:"label"`
	Action      string   `json:"action"` // create, mapped, review
	Reason      string   `json:"reason,omitempty"`
	Revision    int      `json:"revision,omitempty"`
	Candidates  []string `json:"candidates"`
}

type CatalogRegistryKeyAction struct {
	AccountKey  string   `json:"account_key"`
	AccountUUID string   `json:"account_uuid,omitempty"`
	Action      string   `json:"action"` // mapped, review
	Reason      string   `json:"reason,omitempty"`
	Candidates  []string `json:"candidates"`
}

type CatalogRegistryCollectionAction struct {
	CatalogID      string  `json:"catalog_id"`
	CollectionUUID string  `json:"collection_uuid"`
	AccountUUID    *string `json:"account_uuid"`
	Namespace      string  `json:"namespace"`
	Label          string  `json:"label"`
	Action         string  `json:"action"` // create, mapped, review
	Reason         string  `json:"reason,omitempty"`
	RedirectTo     string  `json:"redirect_to,omitempty"`
}

type CatalogRegistryImportRecord struct {
	CatalogIdentityImportRecord
	CollectionUUID *string `json:"collection_uuid,omitempty" db:"collection_uuid"`
}

type CatalogRegistryImportPlan struct {
	UUID               string                            `json:"uuid"`
	SourceUUID         string                            `json:"source_uuid"`
	IdentityImportUUID string                            `json:"identity_import_uuid"`
	InputSHA256        string                            `json:"input_sha256"`
	CapturedAt         string                            `json:"captured_at"`
	RecordCount        int                               `json:"record_count"`
	Inventory          json.RawMessage                   `json:"inventory"`
	Accounts           []CatalogRegistryAccountAction    `json:"accounts"`
	AccountKeys        []CatalogRegistryKeyAction        `json:"account_keys"`
	Collections        []CatalogRegistryCollectionAction `json:"collections"`
	Ownership          []CatalogOwnershipAction          `json:"ownership"`
	Records            []CatalogRegistryImportRecord     `json:"records"`
	PlanSHA256         string                            `json:"plan_sha256"`
}

type CatalogRegistryImport struct {
	CatalogRegistryImportPlan
	CreatedAt time.Time `json:"created_at"`
}

var (
	ErrCatalogRegistryImportInvalid  = errors.New("invalid catalog registry snapshot")
	ErrCatalogRegistryImportConflict = errors.New("catalog registry import or reviewed native state changed")
)

type CatalogRegistryImportReaderWriter interface {
	Preview(context.Context, CatalogRegistryImportInput, time.Time) (*CatalogRegistryImportPlan, error)
	Apply(context.Context, CatalogRegistryImportInput, string, time.Time) (*CatalogRegistryImport, error)
	Find(context.Context, string) (*CatalogRegistryImport, error)
	Records(context.Context, string, int64, int) ([]CatalogRegistryImportRecord, error)
}
