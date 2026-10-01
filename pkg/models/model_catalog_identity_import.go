package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// CatalogIdentityImportInput names one frozen registry snapshot and the local
// Stash namespace it belongs to. Account bindings are explicit native UUIDs;
// neither account handles nor performer names authorize an ownership choice.
type CatalogIdentityImportInput struct {
	UUID            string            `json:"uuid"`
	SourceUUID      string            `json:"source_uuid"`
	Namespace       string            `json:"namespace"`
	Document        json.RawMessage   `json:"document"`
	AccountBindings map[string]string `json:"account_bindings"`
}

type CatalogIdentityAction struct {
	IdentityUUID    string `json:"identity_uuid"`
	Action          string `json:"action"` // adopt, redirect, mapped, review
	Reason          string `json:"reason,omitempty"`
	CurrentUUID     string `json:"current_uuid,omitempty"`
	CurrentRevision int    `json:"current_revision,omitempty"`
	LocalID         *int   `json:"local_id,omitempty"`
	RedirectTo      string `json:"redirect_to,omitempty"`
}

type CatalogOwnershipAction struct {
	AccountKey        string `json:"account_key"`
	AccountUUID       string `json:"account_uuid,omitempty"`
	AccountRevision   int    `json:"account_revision,omitempty"`
	IdentityUUID      string `json:"identity_uuid,omitempty"`
	PerformerRevision int    `json:"performer_revision,omitempty"`
	Action            string `json:"action"` // linked, unlinked, mapped, review
	Reason            string `json:"reason,omitempty"`
	DecisionUUID      string `json:"decision_uuid,omitempty"`
}

type CatalogIdentityImportPlan struct {
	UUID        string                        `json:"uuid"`
	SourceUUID  string                        `json:"source_uuid"`
	Namespace   string                        `json:"namespace"`
	InputSHA256 string                        `json:"input_sha256"`
	CapturedAt  string                        `json:"captured_at"`
	RecordCount int                           `json:"record_count"`
	Inventory   json.RawMessage               `json:"inventory"`
	Identities  []CatalogIdentityAction       `json:"identities"`
	Ownership   []CatalogOwnershipAction      `json:"ownership"`
	Records     []CatalogIdentityImportRecord `json:"records"`
	PlanSHA256  string                        `json:"plan_sha256"`
}

type CatalogIdentityImport struct {
	CatalogIdentityImportPlan
	CreatedAt time.Time `json:"created_at"`
}

type CatalogIdentityImportRecord struct {
	Sequence    int64           `json:"sequence,omitempty" db:"id"`
	Table       string          `json:"table" db:"source_table"`
	SourceKey   string          `json:"source_key" db:"source_key"`
	Outcome     string          `json:"outcome" db:"outcome"`
	Reason      string          `json:"reason,omitempty" db:"reason"`
	ArchiveUUID *string         `json:"archive_uuid,omitempty" db:"archive_uuid"`
	AccountUUID *string         `json:"account_uuid,omitempty" db:"account_uuid"`
	Evidence    json.RawMessage `json:"evidence,omitempty" db:"-"`
}

var (
	ErrCatalogIdentityImportInvalid  = errors.New("invalid catalog identity snapshot")
	ErrCatalogIdentityImportConflict = errors.New("catalog identity import or reviewed native state changed")
)

type CatalogIdentityImportReaderWriter interface {
	Preview(context.Context, CatalogIdentityImportInput, time.Time) (*CatalogIdentityImportPlan, error)
	Apply(context.Context, CatalogIdentityImportInput, string, time.Time) (*CatalogIdentityImport, error)
	Find(context.Context, string) (*CatalogIdentityImport, error)
	Records(context.Context, string, int64, int) ([]CatalogIdentityImportRecord, error)
}
