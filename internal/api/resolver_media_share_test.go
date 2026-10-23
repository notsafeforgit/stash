package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/sharing"
	"github.com/stretchr/testify/require"
)

func TestMediaShareManagementQueriesUseDatabaseTransactions(t *testing.T) {
	rs, _, first, _, db := shareHTTPMediaDatabaseFixture(t, false)
	require.NoError(t, rs.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO images(id,title,created_at,updated_at) VALUES
(2,'Second',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),(3,'Third',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
(4,'Fourth',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO images_files(image_id,file_id,"primary") VALUES(2,2,1),(3,2,1),(4,2,1);`, nil)
		return err
	}))
	rs.service.Repo.Image = db.Repository().Image
	second, _, err := rs.service.Create(t.Context(), "owner", sharing.Options{Label: "Four images", ExpiresAt: time.Now().Add(time.Hour)}, []sharing.Target{
		{Kind: "IMAGE", ID: 1}, {Kind: "IMAGE", ID: 2}, {Kind: "IMAGE", ID: 3}, {Kind: "IMAGE", ID: 4},
	})
	require.NoError(t, err)
	call := nativeResolverCaller(t, &Resolver{repository: rs.service.Repo, shares: rs.service})
	const fields = `id label url preparation_state prepared_count media_count selection_count preparation_error items {id kind title media_count}`
	query := func(document string, variables map[string]any) json.RawMessage {
		t.Helper()
		var response struct {
			Data   map[string]json.RawMessage `json:"data"`
			Errors json.RawMessage            `json:"errors"`
		}
		body := call(document, variables)
		require.NoError(t, json.Unmarshal([]byte(body), &response))
		require.Empty(t, response.Errors, body)
		return response.Data["result"]
	}
	list := func(status string, offset int) []*MediaShare {
		t.Helper()
		body := query(`query($status:MediaShareStatus,$offset:Int!){result:mediaShares(status:$status,limit:10,offset:$offset){`+fields+`}}`, map[string]any{"status": status, "offset": offset})
		var rows []*MediaShare
		require.NoError(t, json.Unmarshal(body, &rows))
		return rows
	}
	rows := list("ACTIVE", 0)
	require.Len(t, rows, 2)
	for _, row := range rows {
		var single MediaShare
		require.NoError(t, json.Unmarshal(query(`query($id:ID!){result:mediaShare(id:$id){`+fields+`}}`, map[string]any{"id": row.ID}), &single))
		require.Equal(t, *row, single, "list and individual reads must expose the same committed share")
		require.Equal(t, SharePreparationStateReady, row.PreparationState)
		require.NotNil(t, row.URL)
		if row.ID == second.ID {
			require.Equal(t, 4, *row.MediaCount)
			require.Equal(t, 4, row.PreparedCount)
			require.Equal(t, 4, row.SelectionCount)
			require.Len(t, row.Items, 3, "management summaries stay bounded independently of selection size")
		}
	}
	require.Len(t, list("ACTIVE", 1), 1)
	require.Empty(t, list("ACTIVE", 2))
	require.JSONEq(t, `true`, string(query(`mutation($id:ID!){result:mediaShareRevoke(id:$id)}`, map[string]any{"id": first.ID})))
	active, inactive := list("ACTIVE", 0), list("INACTIVE", 0)
	require.Len(t, active, 1)
	require.Equal(t, second.ID, active[0].ID)
	require.Len(t, inactive, 1)
	require.Equal(t, first.ID, inactive[0].ID)
	require.Nil(t, inactive[0].URL)
}
