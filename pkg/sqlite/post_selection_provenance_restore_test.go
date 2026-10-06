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

	"github.com/stretchr/testify/require"
)

func TestPostSelectionProvenancePortableRestoreRetainsOriginalCaptureOwnership(t *testing.T) {
	db, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "original")
	b := identityPost(t, repo, "native:reddit", "one-post")
	capture := postSelectionCapture(t, repo, a, 0, 2)
	publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	selected := postSelectionApply(t, repo, b, capture.UUID, "pinned", "review")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		preview, err := repo.SourceGallery.Preview(ctx, b)
		if err != nil {
			return err
		}
		_, err = repo.SourceGallery.Sync(ctx, b, preview.Signature)
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
	command := osexec.CommandContext(ctx, python, "-c", postSelectionRestore, db.DatabasePath(), t.TempDir())
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
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		current, err := repo.SourceAttachment.Selection(ctx, b)
		require.NoError(t, err)
		require.Equal(t, selected, current)
		original, err := repo.SourceEvidence.RecordCapture(ctx, capture)
		require.NoError(t, err)
		require.Equal(t, a, original.PostUUID)
		return nil
	}))
}

const postSelectionRestore = `
from contextlib import closing
import json, sqlite3, sys
from pathlib import Path
from stash_archive.bundle import export_archive, import_archive
database, destination = map(Path, sys.argv[1:])
tables = {'source_posts': 'uuid', 'source_post_identities': 'post_uuid', 'source_post_consolidations': 'sequence',
          'source_captures': 'uuid', 'source_post_revisions': 'uuid', 'source_payloads': 'digest',
          'source_attachments': 'uuid', 'source_attachment_manifests': 'uuid',
          'source_attachment_entries': 'manifest_uuid,position', 'post_attachment_decisions': 'uuid',
          'post_attachment_decision_manifests': 'decision_uuid,manifest_uuid', 'post_attachment_selections': 'post_uuid',
          'post_gallery_decisions': 'uuid', 'post_gallery_links': 'post_uuid', 'galleries': 'id',
          'metadata_field_decisions': 'sequence', 'metadata_field_heads': 'entity_uuid,field'}
def rows(path):
    with closing(sqlite3.connect(path.resolve().as_uri() + '?mode=ro', uri=True)) as db:
        assert db.execute('PRAGMA foreign_key_check').fetchone() is None
        return {t: db.execute(f'SELECT * FROM {t} ORDER BY {order}').fetchall() for t, order in tables.items()}
before = rows(database)
with closing(sqlite3.connect(database.resolve().as_uri() + '?mode=ro', uri=True)) as db:
    assert db.execute('SELECT count(*) FROM post_attachment_decisions d JOIN source_captures c ON c.uuid=d.capture_uuid WHERE d.post_uuid!=c.post_uuid').fetchone() == (1,)
export_archive(database, destination / 'archive', reserve=0)
import_archive(destination / 'archive', destination / 'relocated', reserve=0)
restored = destination / 'relocated' / 'library.sqlite'
assert rows(restored) == before
print(json.dumps({'path': str(restored)}))
`
