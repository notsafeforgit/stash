package metadata

import (
	"context"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sourceadapter"
)

// Collection identity and download order do not choose the preferred text.
// Require the Reddit capture to contain the actual previously selected Redgifs
// item. Similar captions, uploader names or byte equality alone are insufficient.
func (s Service) redditHostedMetadata(ctx context.Context, reddit *models.SourceCapture, previousID string) (bool, error) {
	previous, err := s.Repo.SourceEvidence.FindCapture(ctx, previousID)
	if err != nil || previous == nil || previous.Platform != "redgifs" {
		return false, err
	}
	hostedRaw, err := archive.RestoreCapture(previous.Payload)
	if err != nil {
		return false, err
	}
	hosted, err := sourceadapter.ExtractCapturedPost(hostedRaw)
	if err != nil || hosted == nil || hosted.Namespace != "native:redgifs" {
		return false, nil
	}
	redditRaw, err := archive.RestoreCapture(reddit.Payload)
	if err != nil {
		return false, err
	}
	album, err := sourceadapter.ExtractCapturedAlbum(redditRaw)
	if err != nil || album == nil || album.Post.Namespace != "native:reddit" {
		return false, nil
	}
	for _, entry := range album.Manifest.Entries {
		if entry.Reference == *hosted {
			return true, nil
		}
	}
	return false, nil
}
