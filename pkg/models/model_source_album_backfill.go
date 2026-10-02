package models

import "errors"

const (
	SourceAlbumIdentifiersV1    = "source-identifiers-v1"
	SourceAlbumRedditFilenameV1 = "legacy-reddit-filename-v1"
)

// Filename matching is an explicit historical policy. It binds an existing
// typed attachment to a file observation; it never establishes album order.
func ValidSourceAlbumPolicy(policy string) bool {
	return policy == SourceAlbumIdentifiersV1 || policy == SourceAlbumRedditFilenameV1
}

type SourceAlbumProof struct {
	EvidenceUUID      string  `json:"evidence_uuid"`
	PostFileUUID      string  `json:"post_file_uuid,omitempty"`
	MatchUUID         string  `json:"match_uuid,omitempty"`
	FileUUID          string  `json:"file_uuid,omitempty"`
	Generation        int64   `json:"generation,omitempty"`
	ArchiveFileUUID   *string `json:"archive_file_uuid,omitempty"`
	ArchiveGeneration *int64  `json:"archive_generation,omitempty"`
	RelativePath      string  `json:"relative_path,omitempty"`
	Basis             string  `json:"basis"`  // source-id, legacy-reddit-filename, attachment-evidence
	Status            string  `json:"status"` // valid, file-changed, owner-changed, media-unavailable, evidence-only
}

type SourceAlbumCandidate struct {
	MediaUUID     string             `json:"media_uuid"`
	MediaRevision int                `json:"media_revision"`
	MediaKind     ArchiveEntityKind  `json:"media_kind"`
	Proofs        []SourceAlbumProof `json:"proofs"`
}

type SourceAlbumMatch struct {
	AttachmentUUID     string                 `json:"attachment_uuid"`
	AttachmentRevision int                    `json:"attachment_revision"`
	Reference          SourcePostIdentifier   `json:"reference"`
	DecisionUUID       string                 `json:"decision_uuid,omitempty"`
	Status             string                 `json:"status"` // matched, preserved, ambiguous, review, unavailable
	Reason             string                 `json:"reason,omitempty"`
	Candidates         []SourceAlbumCandidate `json:"candidates"`
}

type SourceAlbumBackfillPreview struct {
	PostUUID  string                `json:"post_uuid"`
	Policy    string                `json:"policy"`
	Signature string                `json:"signature"`
	Gallery   *SourceGalleryPreview `json:"gallery"`
	Matches   []SourceAlbumMatch    `json:"matches"`
}

type SourceAlbumBackfillResult struct {
	Gallery     *SourceGallerySyncResult `json:"gallery"`
	Selected    int                      `json:"selected"`
	Review      int                      `json:"review"`
	Unavailable int                      `json:"unavailable"`
}

var (
	ErrSourceAlbumPolicy = errors.New("invalid source album matching policy")
	ErrSourceAlbumLimit  = errors.New("source album matching exceeds the bounded review limit")
)
