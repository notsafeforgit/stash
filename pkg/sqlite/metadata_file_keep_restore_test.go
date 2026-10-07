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

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestMetadataFileKeepPortableRestoreRetainsOriginalRequestAndFieldState(t *testing.T) {
	f, input := metadataFileReviewFixture(t, `{"title":"Private retained edit"}`)
	receipt, _, err := applyFileEdit(f.repo, keepFileEditRequest(t, f.repo, input))
	require.NoError(t, err)
	before := metadataState(t, f.repo, input.EntityUUID, "title")
	require.NoError(t, f.db.Close())
	python := os.Getenv("PRODUCER_PYTHON")
	if python == "" {
		python = "python3"
	}
	source, err := filepath.Abs("../../integrations/archive/src")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, "-c", metadataFileKeepRestore, f.db.DatabasePath(), t.TempDir())
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
	repo := f.db.Repository()
	replayed, replay, err := applyFileEdit(repo, receipt.Request)
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, receipt, replayed)
	require.Equal(t, before, metadataState(t, repo, input.EntityUUID, "title"))
	require.Empty(t, archiveQueue(t, repo, "metadata", "", 25).Items)
}

const metadataFileKeepRestore = `
from contextlib import closing
import json, sqlite3, sys
from pathlib import Path
from stash_archive.bundle import export_archive, import_archive
database, destination = map(Path, sys.argv[1:])
def rows(path):
    with closing(sqlite3.connect(path.resolve().as_uri()+'?mode=ro', uri=True)) as db:
        assert db.execute('PRAGMA integrity_check').fetchall() == [('ok',)]
        assert db.execute('PRAGMA foreign_key_check').fetchone() is None
        tables = [r[0] for r in db.execute("SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'")]
        return {t: sorted(db.execute('SELECT * FROM "'+t.replace('"','""')+'"').fetchall(), key=repr) for t in tables}
before = rows(database)
assert len(before['metadata_file_edit_keeps']) == 1
export_archive(database, destination/'archive', reserve=0)
import_archive(destination/'archive', destination/'relocated', reserve=0)
restored = destination/'relocated'/'library.sqlite'
assert rows(restored) == before
print(json.dumps({'path':str(restored)}))
`
