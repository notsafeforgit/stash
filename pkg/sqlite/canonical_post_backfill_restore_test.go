//go:build integration

package sqlite_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestCanonicalPostBackfillPortableRestoreKeepsCrossOwnerFileProof(t *testing.T) {
	f, original, _ := postBackfillFixture(t)
	current := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "current-copy"}, "").UUID
	sqlite.ConsolidatePostBackfillFixture(t, f.repo, original, current, "")
	input := postBackfillRequest(previewPostBackfill(t, f.repo, current))
	result, err := applyPostBackfill(f.repo, input)
	require.NoError(t, err)
	require.Equal(t, 1, result.Selected)
	require.NoError(t, f.db.Close())
	python := os.Getenv("PRODUCER_PYTHON")
	if python == "" {
		python = "python3"
	}
	source, err := filepath.Abs("../../integrations/archive/src")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, "-c", canonicalPostBackfillRestore, f.db.DatabasePath(), t.TempDir())
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+source)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	var restored struct {
		Path string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(output, &restored))
	_, err = sqlite.VerifyNativeSnapshot(t.Context(), restored.Path)
	require.NoError(t, err)
	require.NoError(t, f.db.Open(restored.Path))
	f.repo = f.db.Repository()
	for _, mergeAgain := range []bool{false, true} {
		if mergeAgain {
			next := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "later-copy"}, "").UUID
			sqlite.ConsolidatePostBackfillFixture(t, f.repo, current, next, "")
		}
		replay, err := applyPostBackfill(f.repo, input)
		require.NoError(t, err)
		require.Equal(t, result, replay)
	}
}

const canonicalPostBackfillRestore = `
from contextlib import closing
import json,sqlite3,sys
from pathlib import Path
from stash_archive.bundle import export_archive,import_archive
database,destination=map(Path,sys.argv[1:])
tables={'post_media_backfills':'uuid','post_media_backfill_decisions':'backfill_uuid,decision_uuid',
        'post_media_decision_evidence':'decision_uuid,evidence_uuid','post_media_decisions':'uuid',
        'post_media_links':'post_uuid,media_uuid','source_posts':'uuid','source_post_identities':'post_uuid',
        'source_post_consolidations':'sequence','source_media_evidence':'uuid',
        'source_post_file_evidence':'uuid','source_file_observations':'uuid','source_file_matches':'uuid'}
def rows(path):
    with closing(sqlite3.connect(path.resolve().as_uri()+'?mode=ro',uri=True)) as db:
        assert db.execute('PRAGMA integrity_check').fetchall()==[('ok',)]
        assert db.execute('PRAGMA foreign_key_check').fetchone() is None
        assert db.execute('SELECT count(*) FROM post_media_decision_evidence p JOIN post_media_decisions d ON d.uuid=p.decision_uuid JOIN source_media_evidence e ON e.uuid=p.evidence_uuid WHERE d.post_uuid!=e.post_uuid').fetchone()[0]==1
        return {name:db.execute(f'SELECT * FROM {name} ORDER BY {order}').fetchall() for name,order in tables.items()}
before=rows(database)
export_archive(database,destination/'archive',reserve=0)
import_archive(destination/'archive',destination/'relocated',reserve=0)
restored=destination/'relocated'/'library.sqlite'
assert rows(restored)==before
print(json.dumps({'path':str(restored)}))
`
