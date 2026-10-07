package models

// SourceAlbumSelection describes the selected source list, not download
// completion. ExpectedCount may be unknown even when some positions are known.
type SourceAlbumSelection struct {
	UUID          string `json:"uuid"`
	Revision      int    `json:"revision"`
	Mode          string `json:"mode"`
	Complete      bool   `json:"complete"`
	DeclaredAlbum bool   `json:"declared_album"`
	ExpectedCount *int   `json:"expected_count"`
	EntryCount    int    `json:"entry_count"`
}

type SourceAlbumAttachment struct {
	UUID      string                      `json:"uuid"`
	Revision  int                         `json:"revision"`
	Reference SourcePostIdentifierSummary `json:"reference"`
}

// A missing source range is one row with a nil attachment. Repeated attachment
// positions remain separate rows even when they select the same library item.
// RegisteredFiles counts database associations; it does not assert that those
// files are online or that an unselected attachment is currently downloading.
type SourceAlbumSlot struct {
	Position          int                    `json:"position"`
	Through           int                    `json:"through"`
	Attachment        *SourceAlbumAttachment `json:"attachment"`
	MediaKind         string                 `json:"media_kind"`
	SelectionState    string                 `json:"selection_state"`
	DecisionUUID      *string                `json:"decision_uuid"`
	Media             *SourcePostLibraryItem `json:"media"`
	PostLinkState     string                 `json:"post_link_state"`
	GalleryMembership string                 `json:"gallery_membership"`
	RegisteredFiles   int                    `json:"registered_files"`
}

// Signature guards the source list and current association/membership state
// across pages. Compact titles and file counts are live display projections.
type SourceAlbumPage struct {
	RequestedUUID string                `json:"requested_uuid"`
	PostUUID      string                `json:"post_uuid"`
	PostRevision  int                   `json:"post_revision"`
	PostState     string                `json:"post_state"`
	Signature     string                `json:"signature"`
	Selection     *SourceAlbumSelection `json:"selection"`
	Album         *SourcePostAlbum      `json:"album"`
	Slots         []SourceAlbumSlot     `json:"slots"`
	NextAfter     *int                  `json:"next_after"`
}

// Each source post retains its own order after galleries are merged. A shared
// gallery does not establish that two source posts have the same identity.
type SourceGalleryPosts struct {
	RequestedUUID string                `json:"requested_uuid"`
	Gallery       SourcePostLibraryItem `json:"gallery"`
	Posts         []SourcePostSummary   `json:"posts"`
}
