package sqlite

import (
	"context"
	"encoding/json"

	"github.com/stashapp/stash/pkg/models"
)

// Called only for the gallery just created in this transaction. Its initial
// fields come from the selected capture, so they must not be mistaken for a
// library edit or for preserved values of unknown origin.
func recordNewSourceGalleryMetadata(ctx context.Context, entityUUID string, preview *models.SourceGalleryPreview) error {
	var evidence struct {
		CaptureUUID string `db:"capture_uuid"`
		HasTitle    bool   `db:"has_title"`
	}
	if err := dbWrapper.Get(ctx, &evidence, `SELECT c.uuid AS capture_uuid, coalesce(json_extract(r.metadata,'$.title'),'')!='' AS has_title
FROM post_attachment_decisions d JOIN source_captures c ON c.uuid=d.capture_uuid
JOIN source_post_revisions r ON r.uuid=c.revision_uuid
WHERE d.uuid=? AND d.post_uuid=?`, preview.SelectionUUID, preview.PostUUID); err != nil {
		return err
	}
	values := []struct{ field, value string }{{"title", preview.Title}}
	if preview.Details != "" {
		values = append(values, struct{ field, value string }{"details", preview.Details})
	}
	if preview.Date != nil {
		values = append(values, struct{ field, value string }{"date", preview.Date.String()})
	}
	for _, item := range values {
		value, err := json.Marshal(item.value)
		if err != nil {
			return err
		}
		origin, capture := "source", &evidence.CaptureUUID
		if item.field == "title" && !evidence.HasTitle {
			origin, capture = "policy", nil
		}
		if err := insertMetadataDecision(ctx, entityUUID, item.field, "inherit", origin, value, capture, "Initial source album metadata"); err != nil {
			return err
		}
	}
	return nil
}
