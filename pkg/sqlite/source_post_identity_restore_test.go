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

func TestPostIdentityPortableRestoreRetainsChainedReceiptsAndOriginalEvidence(t *testing.T) {
	db, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "first")
	b := identityPost(t, repo, "legacy:catalog:fixture", "second")
	c := identityPost(t, repo, "native:reddit", "one-post")
	original := identityAlbum(t, repo, a)
	identityAlbum(t, repo, b)
	request := identityRequest(t, repo, a, b)
	receipt := publishPostIdentity(t, repo, request)
	publishPostIdentity(t, repo, identityRequest(t, repo, b, c))
	require.NoError(t, db.Close())
	python := os.Getenv("PRODUCER_PYTHON")
	if python == "" {
		python = "python3"
	}
	source, err := filepath.Abs("../../integrations/archive/src")
	require.NoError(t, err)
	// Track the Python implementation in Go's test cache, too.
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
	command := osexec.CommandContext(ctx, python, "-c", postIdentityRestore, db.DatabasePath(), t.TempDir())
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
	require.Equal(t, c, identityRead(t, repo, a).CanonicalUUID)
	require.Equal(t, b, *identityRead(t, repo, a).RedirectTo)
	require.Equal(t, receipt, publishPostIdentity(t, repo, request))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		capture, err := repo.SourceEvidence.RecordCapture(ctx, original)
		require.NoError(t, err)
		require.Equal(t, a, capture.PostUUID)
		return nil
	}))
}

const postIdentityRestore = `
from contextlib import closing
import json, sqlite3, sys
from pathlib import Path
from stash_archive.bundle import export_archive, import_archive
database, destination = map(Path, sys.argv[1:])
tables = {'source_posts': 'uuid', 'source_post_identities': 'post_uuid', 'source_post_consolidations': 'sequence',
          'source_post_consolidation_context': 'request_uuid', 'source_post_identifiers': 'namespace,value',
          'source_captures': 'uuid', 'source_payloads': 'digest', 'source_post_revisions': 'uuid',
          'source_attachment_manifests': 'uuid', 'source_attachment_entries': 'manifest_uuid,position',
          'post_attachment_decisions': 'uuid', 'post_attachment_selections': 'post_uuid',
          'post_gallery_decisions': 'uuid', 'post_gallery_links': 'post_uuid', 'galleries': 'id'}
def rows(path):
    with closing(sqlite3.connect(path.resolve().as_uri() + '?mode=ro', uri=True)) as db:
        return {t: db.execute(f'SELECT * FROM {t} ORDER BY {order}').fetchall() for t, order in tables.items()}
before = rows(database)
assert len(before['source_post_consolidations']) == 2
assert len(before['source_post_identities']) == 3
assert before['source_post_consolidation_context'] == []
assert len(before['post_gallery_links']) == 2
export_archive(database, destination / 'archive', reserve=0)
import_archive(destination / 'archive', destination / 'relocated', reserve=0)
restored = destination / 'relocated' / 'library.sqlite'
assert rows(restored) == before
print(json.dumps({'path': str(restored)}))
`
