package models

type EntityImageInput struct {
	Data        *string          `json:"data"`
	ImageID     *string          `json:"image_id"`
	PerformerID *string          `json:"performer_id"`
	Scene       *SceneImageInput `json:"scene"`
}

type SceneImageInput struct {
	ID string   `json:"id"`
	At *float64 `json:"at"`
}
