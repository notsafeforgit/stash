package urlbuilders

import (
	"strconv"

	"github.com/stashapp/stash/pkg/models"
)

type StudioURLBuilder struct {
	BaseURL   string
	StudioID  string
	UpdatedAt string
}

func NewStudioURLBuilder(baseURL string, studio *models.Studio, publicID string) StudioURLBuilder {
	return StudioURLBuilder{
		BaseURL:   baseURL,
		StudioID:  publicID,
		UpdatedAt: strconv.FormatInt(studio.UpdatedAt.Unix(), 10),
	}
}

func (b StudioURLBuilder) GetStudioImageURL(hasImage bool) string {
	url := b.BaseURL + "/studio/" + b.StudioID + "/image?t=" + b.UpdatedAt
	if !hasImage {
		url += "&default=true"
	}
	return url
}
