package models

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

const MaxPostConsolidationReviewBytes = 4 * 1024 * 1024

// Omitted choices preserve compatible current intent. Explicit choices resolve
// the complete reviewed group, including heads on earlier post identities.
type PostConsolidationReviewInput struct {
	SourceUUID      string                              `json:"source_uuid"`
	DestinationUUID string                              `json:"destination_uuid"`
	Selection       *PostConsolidationSelectionChoice   `json:"selection,omitempty"`
	Gallery         *PostConsolidationGalleryChoice     `json:"gallery,omitempty"`
	Media           []PostConsolidationMediaChoice      `json:"media"`
	Attachments     []PostConsolidationAttachmentChoice `json:"attachments"`
	Reason          string                              `json:"reason"`
}

// Choose retains one original selection; combine uses every currently selected
// non-disabled source list and the named selection's original primary capture.
// Disable requires no decision UUID. Arbitrary historical captures are selected
// through the ordinary source-list editor before preparing a merge.
type PostConsolidationSelectionChoice struct {
	Mode         string `json:"mode"` // choose, combine, disabled
	DecisionUUID string `json:"decision_uuid,omitempty"`
}

type PostConsolidationGalleryChoice struct {
	State       string `json:"state"` // linked, disabled
	GalleryUUID string `json:"gallery_uuid,omitempty"`
}

type PostConsolidationMediaChoice struct {
	MediaUUID string `json:"media_uuid"`
	State     string `json:"state"` // linked, unlinked, undecided
}

type PostConsolidationAttachmentChoice struct {
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
	State     string `json:"state"` // linked, unlinked, undecided
	MediaUUID string `json:"media_uuid,omitempty"`
}

type PostConsolidationSelectionPlan struct {
	Mode          string   `json:"mode"`
	CaptureUUID   *string  `json:"capture_uuid"`
	ManifestUUIDs []string `json:"manifest_uuids"`
	EntryCount    int      `json:"entry_count"`
	Complete      bool     `json:"complete"`
	DeclaredAlbum bool     `json:"declared_album"`
	ExpectedCount *int     `json:"expected_count"`
}

type PostConsolidationReviewBlocker struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Value     string `json:"value,omitempty"`
	MediaUUID string `json:"media_uuid,omitempty"`
}

type PostConsolidationAlbumEntry struct {
	Position  int    `json:"position"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
	MediaUUID string `json:"media_uuid,omitempty"`
	Status    string `json:"status"`
}

// Only a newly created gallery receives source text. Existing gallery metadata
// is retained, and membership changes use the ordinary cover/manual-edit rules.
type PostConsolidationAlbumPlan struct {
	Action      string                        `json:"action"`
	GalleryUUID string                        `json:"gallery_uuid,omitempty"`
	Title       string                        `json:"title,omitempty"`
	Details     string                        `json:"details,omitempty"`
	Date        string                        `json:"date,omitempty"`
	Entries     []PostConsolidationAlbumEntry `json:"entries"`
	Add         []SourcePostComparisonEntity  `json:"add"`
	Remove      []SourcePostComparisonEntity  `json:"remove"`
}

// Original choices occur only in Posts; proposed choices contain compact IDs
// and states. Raw source payloads, profiles and plugin settings are not loaded.
type PostConsolidationReviewPreview struct {
	Source      SourcePostIdentity                  `json:"source"`
	Destination SourcePostIdentity                  `json:"destination"`
	Posts       []SourcePostComparisonState         `json:"posts"`
	Selection   *PostConsolidationSelectionPlan     `json:"selection"`
	Gallery     *PostConsolidationGalleryChoice     `json:"gallery"`
	Album       *PostConsolidationAlbumPlan         `json:"album"`
	Media       []PostConsolidationMediaChoice      `json:"media"`
	Attachments []PostConsolidationAttachmentChoice `json:"attachments"`
	Blockers    []PostConsolidationReviewBlocker    `json:"blockers"`
	Ready       bool                                `json:"ready"`
	Digest      string                              `json:"digest"`
}

type PostConsolidationReviewApplyInput struct {
	PostConsolidationReviewInput
	RequestUUID string `json:"request_uuid"`
	Digest      string `json:"digest"`
}

type PostConsolidationReviewMember struct {
	PostUUID              string `json:"post_uuid"`
	PreviousCanonicalUUID string `json:"previous_canonical_uuid"`
	PreviousRevision      int    `json:"previous_revision"`
}

type PostConsolidationGalleryResult struct {
	GalleryUUID string   `json:"gallery_uuid,omitempty"`
	Action      string   `json:"action"`
	Created     bool     `json:"created"`
	Added       []string `json:"added"`
	Removed     []string `json:"removed"`
}

func (r PostConsolidationGalleryResult) Changed() bool {
	return r.Created || len(r.Added) != 0 || len(r.Removed) != 0
}

type PostConsolidationReviewResult struct {
	Consolidation           SourcePostConsolidation         `json:"consolidation"`
	Members                 []PostConsolidationReviewMember `json:"members"`
	SelectionUUID           string                          `json:"selection_uuid,omitempty"`
	GalleryDecisionUUID     string                          `json:"gallery_decision_uuid,omitempty"`
	MediaDecisionUUIDs      []string                        `json:"media_decision_uuids"`
	AttachmentDecisionUUIDs []string                        `json:"attachment_decision_uuids"`
	Gallery                 PostConsolidationGalleryResult  `json:"gallery"`
	NotificationJobUUID     string                          `json:"notification_job_uuid,omitempty"`
}

type PostConsolidationReview struct {
	Request PostConsolidationReviewApplyInput `json:"request"`
	Result  PostConsolidationReviewResult     `json:"result"`
}

type PostConsolidationNotification struct {
	Version               int    `json:"version"`
	ReviewUUID            string `json:"review_uuid"`
	ResumeFromJobUUID     string `json:"resume_from_job_uuid,omitempty"`
	ResumeFromJobRevision int64  `json:"resume_from_job_revision,omitempty"`
}

// Arguments binds explicit retries to the exact terminal job the user reviewed.
// Every retry still delivers the original merge event; it never republishes it.
func (w PostConsolidationNotification) Arguments() (json.RawMessage, error) {
	validID := func(value string) bool {
		id, err := uuid.Parse(value)
		return err == nil && id != uuid.Nil && id.String() == value
	}
	if w.Version != 1 || !validID(w.ReviewUUID) ||
		(w.ResumeFromJobUUID == "" && w.ResumeFromJobRevision != 0) ||
		(w.ResumeFromJobUUID != "" && (!validID(w.ResumeFromJobUUID) || w.ResumeFromJobRevision < 1)) {
		return nil, ErrPostConsolidationReviewInvalid
	}
	value := map[string]any{"version": w.Version, "review_uuid": w.ReviewUUID}
	if w.ResumeFromJobUUID != "" {
		value["resume_from_job_uuid"], value["resume_from_job_revision"] = w.ResumeFromJobUUID, w.ResumeFromJobRevision
	}
	return json.Marshal(value)
}

func ParsePostConsolidationNotification(raw json.RawMessage) (*PostConsolidationNotification, error) {
	var work PostConsolidationNotification
	if len(raw) > 4096 || json.Unmarshal(raw, &work) != nil {
		return nil, ErrPostConsolidationReviewInvalid
	}
	canonical, err := work.Arguments()
	if err != nil || !bytes.Equal(raw, canonical) {
		return nil, ErrPostConsolidationReviewInvalid
	}
	return &work, nil
}

var (
	ErrPostConsolidationReviewInvalid  = errors.New("invalid post consolidation review")
	ErrPostConsolidationReviewConflict = errors.New("post merge choices changed; review a fresh preview")
)
