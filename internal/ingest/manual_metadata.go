package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/models"
)

// ManualFileMetadata is user-supplied metadata for a local upload, not a scraped
// post. Omitted/empty values make no change. Admission binds it to the file's
// preview and durable request, and publication applies it in the media transaction.
type ManualFileMetadata struct {
	Title   string `json:"title,omitempty"`
	Details string `json:"details,omitempty"`
	Date    string `json:"date,omitempty"`
}

func validManualFileMetadata(m *ManualFileMetadata) bool {
	if m == nil {
		return true
	}
	if len(m.Title) > 4096 || len(m.Details) > 16384 || !utf8.ValidString(m.Title) || !utf8.ValidString(m.Details) ||
		strings.ContainsRune(m.Title, 0) || strings.ContainsRune(m.Details, 0) {
		return false
	}
	if m.Date != "" {
		parsed, err := time.Parse("2006-01-02", m.Date)
		if err != nil || parsed.Format("2006-01-02") != m.Date || parsed.Year() < 1 {
			return false
		}
	}
	return true
}

func applyManualFileMetadata(ctx context.Context, repo models.Repository, m *ManualFileMetadata, media *PublishedMedia) (fields, review []string, err error) {
	if m == nil {
		return nil, nil, nil
	}
	for _, item := range []struct{ name, value string }{{"title", m.Title}, {"details", m.Details}, {"date", m.Date}} {
		if item.value == "" {
			continue
		}
		current, err := repo.MetadataField.State(ctx, media.Media.UUID, item.name)
		if err != nil {
			return nil, nil, err
		}
		value, err := json.Marshal(item.value)
		if err != nil {
			return nil, nil, err
		}
		if bytes.Equal(value, current.Value) {
			continue
		}
		// A duplicate upload must never replace a later edit, including a deliberate
		// clear. An automatically derived filename remains eligible for replacement.
		blank := bytes.Equal(current.Value, []byte(`""`)) || bytes.Equal(current.Value, []byte("null"))
		if !media.Created && (current.Protected || (!blank && current.Origin != "filename")) {
			review = append(review, "metadata:"+item.name)
			continue
		}
		_, err = repo.MetadataField.Decide(ctx, models.MetadataFieldDecisionInput{
			EntityUUID: media.Media.UUID, ExpectedEntityRevision: current.Entity.Revision,
			Field: item.name, Mode: "set", Value: value, Origin: "review",
			Reason: "Metadata supplied with local file import",
		})
		if err != nil {
			return nil, nil, err
		}
		fields = append(fields, item.name)
	}
	return fields, review, nil
}
