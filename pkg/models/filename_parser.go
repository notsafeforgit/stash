package models

type SceneParserInput struct {
	IgnoreWords          []string `json:"ignoreWords"`
	WhitespaceCharacters *string  `json:"whitespaceCharacters"`
	CapitalizeTitle      *bool    `json:"capitalizeTitle"`
	IgnoreOrganized      *bool    `json:"ignoreOrganized"`
}

type SceneParserResult struct {
	Scene        *Scene          `json:"scene"`
	Title        *string         `json:"title"`
	Code         *string         `json:"code"`
	Details      *string         `json:"details"`
	Director     *string         `json:"director"`
	URL          *string         `json:"url"`
	Date         *string         `json:"date"`
	Rating       *int            `json:"rating"`
	Rating100    *int            `json:"rating100"`
	StudioID     *string         `json:"studio_id"`
	GalleryIds   []string        `json:"gallery_ids"`
	PerformerIds []string        `json:"performer_ids"`
	Groups       []*SceneGroupID `json:"groups"`
	TagIds       []string        `json:"tag_ids"`
}

type SceneGroupID struct {
	GroupID    string  `json:"group_id"`
	SceneIndex *string `json:"scene_index"`
}
