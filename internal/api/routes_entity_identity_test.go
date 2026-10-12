package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestEntityIdentityURLsFollowMergesWithoutReusingDeletedIdentities(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "identity.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var source, target *models.ArchiveEntity
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO performers(id,created_at,updated_at) VALUES(1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),(2,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO performer_names(performer_id,name,position) VALUES(1,'Before merge',0),(2,'After merge',0);`, nil)
		if err != nil {
			return err
		}
		source, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchivePerformer, 1)
		if err != nil {
			return err
		}
		target, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchivePerformer, 2)
		return err
	}))
	// Identity resolution is core functionality, including without source tracking.
	handler := (&nativeArchiveRoutes{repo: repo}).routerWithSourceTracking(false)
	get := func(kind, id string, status int) metadataEntityIdentity {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/entity-identities/"+kind+"/"+id, nil))
		require.Equal(t, status, w.Code, w.Body.String())
		var identity metadataEntityIdentity
		if status == http.StatusOK {
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &identity))
		}
		return identity
	}
	require.Equal(t, source.UUID, get("performer", "1", http.StatusOK).UUID)
	require.Equal(t, source.UUID, get("performer", source.UUID, http.StatusOK).UUID)
	get("scene", source.UUID, http.StatusNotFound)
	get("performer", uuid.NewString(), http.StatusNotFound)
	get("performer", "invalid", http.StatusBadRequest)
	get("performer", "0", http.StatusBadRequest)
	get("unknown", source.UUID, http.StatusBadRequest)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return repo.Performer.Merge(ctx, []int{1}, 2)
	}))
	merged := get("performer", source.UUID, http.StatusOK)
	require.Equal(t, target.UUID, merged.UUID)
	require.Equal(t, 2, *merged.LocalID)
	get("performer", "1", http.StatusNotFound)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := repo.Performer.Destroy(ctx, 2); err != nil {
			return err
		}
		_, _, err := db.ExecSQL(ctx, `INSERT INTO performers(id,created_at,updated_at) VALUES(2,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO performer_names(performer_id,name,position) VALUES(2,'Unrelated new performer',0);`, nil)
		return err
	}))
	get("performer", source.UUID, http.StatusNotFound)
	get("performer", target.UUID, http.StatusNotFound)
	require.NotEqual(t, target.UUID, get("performer", "2", http.StatusOK).UUID)
}
