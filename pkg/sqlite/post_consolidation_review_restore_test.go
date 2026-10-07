//go:build integration

package sqlite

import (
	"context"
	"encoding/json"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestPostConsolidationReviewPortableRestoreRecoversRequestAndPendingNotification(t *testing.T) {
	db, repo, review := postMergeNotificationFixture(t)
	service := gallery.NewPostMergeNotifications(repo)
	parent, err := service.Find(t.Context(), review.Result.NotificationJobUUID)
	require.NoError(t, err)
	parent, err = service.Cancel(t.Context(), parent.UUID, parent.Revision)
	require.NoError(t, err)
	request := uuid.NewString()
	pending, err := service.Retry(t.Context(), request, parent.UUID, parent.Revision)
	require.NoError(t, err)
	before := identityRows(t, repo, postMergeNotificationDomainTables...)
	require.NoError(t, db.Close())
	python := os.Getenv("PRODUCER_PYTHON")
	if python == "" {
		python = "python3"
	}
	source, err := filepath.Abs("../../integrations/archive/src")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := osexec.CommandContext(ctx, python, "-c", postConsolidationReviewRestore, db.DatabasePath(), t.TempDir())
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
	recovered, replayed := applyConsolidationReview(t, repo, review.Request)
	require.True(t, replayed)
	require.Equal(t, review, recovered)
	service = gallery.NewPostMergeNotifications(repo)
	retry, err := service.Retry(t.Context(), request, parent.UUID, parent.Revision)
	require.NoError(t, err)
	require.Equal(t, pending, retry)
	worker := gallery.NewPostMergeNotificationWorker(service, func(ctx context.Context, original models.PostConsolidationReview, guard gallery.AlbumEffectGuard) error {
		require.Equal(t, *review, original)
		return guard(ctx)
	})
	processed, err := worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	finished, err := service.Find(t.Context(), pending.UUID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", finished.State)
	require.Equal(t, before, identityRows(t, repo, postMergeNotificationDomainTables...))
}

const postConsolidationReviewRestore = `
from contextlib import closing
import json, sqlite3, sys
from pathlib import Path
from stash_archive.bundle import export_archive, import_archive
database, destination = map(Path, sys.argv[1:])
tables = ('source_posts', 'source_post_identities', 'source_post_consolidations', 'source_post_consolidation_context',
          'source_captures', 'source_payloads', 'source_post_revisions', 'source_attachments',
          'source_attachment_manifests', 'source_attachment_entries', 'post_attachment_decisions',
          'post_attachment_decision_manifests', 'post_attachment_selections', 'post_gallery_decisions', 'post_gallery_links',
          'galleries', 'archive_entities', 'post_media_decisions', 'attachment_media_decisions', 'gallery_membership_events',
          'post_consolidation_reviews', 'post_consolidation_review_members', 'post_consolidation_review_media',
          'post_consolidation_review_attachments', 'archive_jobs', 'archive_job_submissions', 'archive_job_attempts')
def rows(path):
    with closing(sqlite3.connect(path.resolve().as_uri()+'?mode=ro', uri=True)) as db:
        assert db.execute('PRAGMA integrity_check').fetchall() == [('ok',)]
        assert db.execute('PRAGMA foreign_key_check').fetchone() is None
        return {t: sorted(db.execute(f'SELECT * FROM {t}').fetchall(), key=repr) for t in tables}
before = rows(database)
assert len(before['post_consolidation_reviews']) == 1
assert len(before['archive_jobs']) == 2
export_archive(database, destination/'archive', reserve=0)
import_archive(destination/'archive', destination/'relocated', reserve=0)
restored = destination/'relocated'/'library.sqlite'
assert rows(restored) == before
print(json.dumps({'path':str(restored)}))
`
