package models

const SourcePostMediaCatalogFilesV1 = "catalog-files-v1"

// A post/file match establishes no attachment identity or ordering.
type SourcePostMediaMatchProof struct {
	SourceAlbumProof
	ObservationUUID string `json:"observation_uuid,omitempty"`
}

type SourcePostMediaMatchCandidate struct {
	MediaUUID        string                      `json:"media_uuid"`
	MediaRevision    int                         `json:"media_revision"`
	MediaKind        ArchiveEntityKind           `json:"media_kind"`
	MediaState       ArchiveEntityState          `json:"media_state"`
	AssociationState string                      `json:"association_state"`
	Status           string                      `json:"status"`
	Reason           string                      `json:"reason,omitempty"`
	Decisions        []SourcePostMediaDecision   `json:"decisions"`
	Proofs           []SourcePostMediaMatchProof `json:"proofs"`
}

type SourcePostMediaMatchPreview struct {
	PostUUID     string                          `json:"post_uuid"`
	PostRevision int                             `json:"post_revision"`
	PostState    string                          `json:"post_state"`
	Policy       string                          `json:"policy"`
	Signature    string                          `json:"signature"`
	Candidates   []SourcePostMediaMatchCandidate `json:"candidates"`
}

type SourcePostMediaBackfillInput struct {
	UUID      string `json:"uuid"`
	PostUUID  string `json:"post_uuid"`
	Signature string `json:"signature"`
}

type SourcePostMediaBackfillResult struct {
	UUID        string                    `json:"uuid"`
	PostUUID    string                    `json:"post_uuid"`
	Signature   string                    `json:"signature"`
	Selected    int                       `json:"selected"`
	Preserved   int                       `json:"preserved"`
	Review      int                       `json:"review"`
	Unavailable int                       `json:"unavailable"`
	Decisions   []SourcePostMediaDecision `json:"decisions"`
}

type SourcePostMediaMatchedEvidence struct {
	EvidenceUUID string `json:"evidence_uuid" db:"evidence_uuid"`
	PostFileUUID string `json:"post_file_uuid" db:"post_file_uuid"`
	MatchUUID    string `json:"match_uuid" db:"match_uuid"`
}
