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

func TestNativePerformerSourcesHTTPScopesAndMergedOwnership(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "performer-sources.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var source, target *models.ArchiveEntity
	var account *models.SourceAccount
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO performers(id,created_at,updated_at) VALUES(1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),(2,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO performer_names(performer_id,name,position) VALUES(1,'Former name',0),(2,'Current name',0);`, nil)
		if err != nil {
			return err
		}
		source, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchivePerformer, 1)
		if err != nil {
			return err
		}
		target, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchivePerformer, 2)
		if err != nil {
			return err
		}
		account, err = repo.SourceAccount.Create(ctx, "native:instagram", "Account")
		if err != nil {
			return err
		}
		_, err = repo.SourceAccount.DecideOwnership(ctx, models.AccountOwnershipInput{AccountUUID: account.UUID, ExpectedAccountRevision: account.Revision,
			State: models.AccountOwnershipLinked, PerformerUUID: source.UUID, ExpectedPerformerRevision: source.Revision, Origin: "review"})
		if err != nil {
			return err
		}
		return repo.Performer.Merge(ctx, []int{1}, 2)
	}))
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	w := get("/entities/" + source.UUID + "/source-accounts")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var page models.PerformerSourceAccounts
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Equal(t, source.UUID, page.RequestedUUID)
	require.Equal(t, target.UUID, page.Performer.UUID)
	require.Equal(t, "Current name", page.Performer.Name)
	require.Len(t, page.Accounts, 1)
	require.Equal(t, account.UUID, page.Accounts[0].UUID)
	w = get("/entities/" + source.UUID + "/source-accounts?after=" + account.UUID)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.NotNil(t, page.Accounts)
	require.Empty(t, page.Accounts)
	w = get("/entities/" + source.UUID + "/performer-identities?limit=1")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var identities models.PerformerSourceIdentities
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &identities))
	require.Equal(t, target.UUID, identities.Performer.UUID)
	require.Len(t, identities.Identities, 1)
	first := identities.Identities[0].UUID
	w = get("/entities/" + source.UUID + "/performer-identities?limit=1&after=" + first)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &identities))
	require.Len(t, identities.Identities, 1)
	require.Greater(t, identities.Identities[0].UUID, first)
	for _, endpoint := range []string{"source-accounts", "performer-identities"} {
		for _, query := range []string{"?limit=0", "?limit=101", "?after=bad"} {
			w = get("/entities/" + source.UUID + "/" + endpoint + query)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		}
		require.Equal(t, http.StatusBadRequest, get("/entities/invalid/"+endpoint).Code)
		require.Equal(t, http.StatusNotFound, get("/entities/"+uuid.NewString()+"/"+endpoint).Code)
	}
}
