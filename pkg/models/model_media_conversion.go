package models

import (
	"context"
	"time"
)

// MediaConversion retains the typed identities and file lifetimes on both sides
// of a verified postprocessor conversion. It does not assert byte equality.
type MediaConversion struct {
	UUID               string    `json:"uuid" db:"uuid"`
	ImageUUID          string    `json:"image_uuid" db:"image_uuid"`
	SceneUUID          string    `json:"scene_uuid" db:"scene_uuid"`
	OriginalFileUUID   string    `json:"original_file_uuid" db:"original_file_uuid"`
	OriginalGeneration int64     `json:"original_generation" db:"original_generation"`
	FileUUID           string    `json:"file_uuid" db:"file_uuid"`
	Generation         int64     `json:"generation" db:"generation"`
	Photographer       string    `json:"photographer" db:"photographer"`
	UndatedOCount      int       `json:"undated_o_count" db:"undated_o_count"`
	CreatedAt          time.Time `json:"created_at"`
}

type ImageConversionInput struct {
	UUID                  string
	ImageUUID             string
	ExpectedImageRevision int
	SceneUUID             string
	ExpectedSceneRevision int
	OriginalFileUUID      string
	OriginalGeneration    int64
	FileUUID              string
	Generation            int64
}

type MediaConversionReaderWriter interface {
	Find(context.Context, string) (*MediaConversion, error)
	// ConvertImage runs only after the intake service verifies the converted
	// video, original path absence, file lifetime and selected source attachment.
	// The destination must be newly created and have no metadata choices.
	ConvertImage(context.Context, ImageConversionInput) (*MediaConversion, error)
}
