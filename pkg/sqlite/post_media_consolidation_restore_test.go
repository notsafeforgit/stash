//go:build integration

package sqlite

import (
	"context"
	"encoding/json"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestPostMediaConsolidationPortableRestorePreservesChoicesAndOriginalReceipts(t *testing.T) {
	db, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "original")
	b := identityPost(t, repo, "native:reddit", "one-post")
	capture := identityAlbum(t, repo, a)
	identityAlbum(t, repo, b)
	originalGallery := consolidationGalleryChoice(t, repo, a)
	media := consolidationTestMedia(t, repo)
	originalRequest := consolidationMediaRequest(t, repo, a, media.UUID, "linked", false)
	original := applyConsolidationMedia(t, repo, originalRequest, "")
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	request := consolidationMediaRequest(t, repo, b, media.UUID, "linked", true)
	selected := applyConsolidationMedia(t, repo, request, merge.UUID)
	applyConsolidationSelection(t, repo, b, capture.UUID, "pinned", merge.UUID)
	galleryInput, expected := consolidationGalleryRequest(t, repo, b, *originalGallery.GalleryUUID)
	var galleryChoice *models.SourceGalleryDecision
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		galleryChoice, err = publishConsolidatedPostGallery(ctx, galleryInput, merge.UUID, expected)
		if err != nil {
			return err
		}
		entity, err := repo.ArchiveEntity.Find(ctx, media.UUID)
		if err != nil {
			return err
		}
		field, err := repo.MetadataField.ApplyAutomatic(ctx, models.MetadataFieldDecisionInput{EntityUUID: media.UUID,
			ExpectedEntityRevision: entity.Revision, Field: "details", Mode: "inherit", Origin: "source",
			CaptureUUID: capture.UUID, Value: json.RawMessage(`"Original caption"`)})
		if err != nil {
			return err
		}
		_, err = dbWrapper.Exec(ctx, "INSERT INTO metadata_decision_post_media(decision_uuid,post_media_decision_uuid) VALUES(?,?)", field.Decision.UUID, selected.UUID)
		return err
	}))
	require.NoError(t, db.Close())
	python := os.Getenv("PRODUCER_PYTHON")
	if python == "" {
		python = "python3"
	}
	source, err := filepath.Abs("../../integrations/archive/src")
	require.NoError(t, err)
	entries, err := os.ReadDir(filepath.Join(source, "stash_archive"))
	require.NoError(t, err)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".py") {
			_, err := os.ReadFile(filepath.Join(source, "stash_archive", entry.Name()))
			require.NoError(t, err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := osexec.CommandContext(ctx, python, "-c", postMediaConsolidationRestore, db.DatabasePath(), t.TempDir())
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+source)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	var restored struct {
		Path string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(output, &restored))
	_, err = VerifyNativeSnapshot(t.Context(), restored.Path)
	require.NoError(t, err)
	require.NoError(t, db.Open(restored.Path))
	repo = db.Repository()
	require.Equal(t, original, applyConsolidationMedia(t, repo, originalRequest, ""))
	require.Equal(t, selected, applyConsolidationMedia(t, repo, request, merge.UUID))
	require.Equal(t, galleryChoice, consolidationGalleryChoice(t, repo, b))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		originalCapture, err := repo.SourceEvidence.RecordCapture(ctx, capture)
		require.NoError(t, err)
		require.Equal(t, a, originalCapture.PostUUID)
		require.NoError(t, repo.SourcePostMedia.ValidateCapture(ctx, selected.UUID, capture.UUID, media.UUID))
		field, err := repo.MetadataField.State(ctx, media.UUID, "details")
		require.NoError(t, err)
		require.Equal(t, capture.UUID, *field.Decision.CaptureUUID)
		require.Equal(t, selected.UUID, field.Decision.PostMediaDecisionUUID)
		return nil
	}))
}

const postMediaConsolidationRestore = `
from contextlib import closing
import json, sqlite3, sys
from pathlib import Path
from stash_archive.bundle import export_archive, import_archive
database, destination = map(Path, sys.argv[1:])
tables = {'source_posts':'uuid', 'source_post_identities':'post_uuid', 'source_post_consolidations':'sequence',
          'source_captures':'uuid', 'source_post_revisions':'uuid', 'source_payloads':'digest',
          'post_media_decisions':'uuid', 'post_media_links':'post_uuid,media_uuid',
          'post_media_supersessions':'previous_uuid', 'post_media_consolidation_edges':'previous_uuid',
          'post_attachment_decisions':'uuid', 'post_attachment_decision_manifests':'decision_uuid,manifest_uuid',
          'post_attachment_selections':'post_uuid', 'post_gallery_decisions':'uuid',
          'post_gallery_links':'post_uuid', 'galleries':'id', 'metadata_field_decisions':'sequence',
          'metadata_field_heads':'entity_uuid,field', 'metadata_decision_post_media':'decision_uuid'}
def rows(path):
    with closing(sqlite3.connect(path.resolve().as_uri()+'?mode=ro', uri=True)) as db:
        assert db.execute('PRAGMA integrity_check').fetchall() == [('ok',)]
        assert db.execute('PRAGMA foreign_key_check').fetchone() is None
        return {t: db.execute(f'SELECT * FROM {t} ORDER BY {order}').fetchall() for t,order in tables.items()}
before = rows(database)
assert len(before['post_media_consolidation_edges']) == 1
assert len(before['metadata_decision_post_media']) == 1
assert len(before['post_gallery_decisions']) == 3 and len(before['post_gallery_links']) == 1
export_archive(database, destination/'archive', reserve=0)
import_archive(destination/'archive', destination/'relocated', reserve=0)
restored = destination/'relocated'/'library.sqlite'
assert rows(restored) == before
print(json.dumps({'path':str(restored)}))
`
