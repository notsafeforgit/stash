package models

// Gallery consolidation joins source albums, never the identities of their
// posts or media. The complete connected component is previewed atomically.
type GalleryConsolidationMember struct {
	UUID     string `json:"uuid"`
	Revision int    `json:"revision"`
	Title    string `json:"title"`
	LocalID  int    `json:"-"`
}

type GalleryConsolidationPreview struct {
	GalleryUUID string                       `json:"gallery_uuid"`
	Galleries   []GalleryConsolidationMember `json:"galleries"`
	ImageCount  int                          `json:"image_count"`
	SceneCount  int                          `json:"scene_count"`
	Conflicts   []string                     `json:"conflicts"`
	Signature   string                       `json:"signature"`
}

type GalleryConsolidationResult struct {
	GalleryUUID string   `json:"gallery_uuid"`
	MergedUUIDs []string `json:"merged_uuids"`
	Added       []string `json:"added"`
}
