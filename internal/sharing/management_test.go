package sharing

import (
	"context"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestShareDeleteRejectsActiveGrants(t *testing.T) {
	s, _, file, _ := testService(t)
	ctx := context.Background()
	row, secret := createTestShare(t, s)
	session, _, err := s.Exchange(ctx, row.ID, secret)
	require.NoError(t, err)
	delivery, done, err := s.BeginDelivery(ctx, row)
	require.NoError(t, err)
	defer done()
	require.ErrorIs(t, s.Delete(ctx, row.ID), ErrActive)
	require.NoError(t, delivery.Err())
	_, err = s.Authenticate(ctx, row.ID, session)
	require.NoError(t, err)
	require.FileExists(t, file.Path)
}

func TestShareDeleteInactiveGrantsAndSessions(t *testing.T) {
	for _, status := range []string{"revoked", "expired"} {
		t.Run(status, func(t *testing.T) {
			s, _, file, _ := testService(t)
			ctx := context.Background()
			row, secret := createTestShare(t, s)
			other, otherSecret := createTestShare(t, s)
			session, _, err := s.Exchange(ctx, row.ID, secret)
			require.NoError(t, err)
			preview, err := s.Preview(ctx, row.ID)
			require.NoError(t, err)
			if status == "revoked" {
				require.NoError(t, s.Revoke(ctx, row.ID))
			} else {
				require.NoError(t, s.Repo.WithTxn(ctx, func(ctx context.Context) error {
					row.ExpiresAt = time.Now().Unix()
					return s.Repo.Share.Update(ctx, row)
				}))
			}
			var invalidated []string
			s.OnInvalidate = func(id string) { invalidated = append(invalidated, id) }
			require.NoError(t, s.Delete(ctx, row.ID))
			require.Equal(t, []string{row.ID}, invalidated)
			saved, err := s.Find(ctx, row.ID)
			require.NoError(t, err)
			require.Nil(t, saved)
			require.NoError(t, s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
				for _, token := range []string{session, preview} {
					saved, err := s.Repo.Share.FindSession(ctx, Hash(token))
					require.NoError(t, err)
					require.Nil(t, saved)
				}
				return nil
			}))
			_, err = s.Authenticate(ctx, row.ID, session)
			require.ErrorIs(t, err, ErrUnavailable)
			_, _, err = s.Exchange(ctx, row.ID, secret)
			require.ErrorIs(t, err, ErrUnavailable)
			_, err = s.Update(ctx, row.ID, Options{Label: "Restore", ExpiresAt: time.Now().Add(time.Hour)})
			require.ErrorIs(t, err, ErrUnavailable)
			_, _, err = s.Exchange(ctx, other.ID, otherSecret)
			require.NoError(t, err, "other grants keep working")
			require.NoError(t, s.Delete(ctx, row.ID), "repeated deletion is harmless")
			require.FileExists(t, file.Path, "deleting a grant never deletes media")
		})
	}
}

func TestShareListFiltersBeforePagination(t *testing.T) {
	repo := testShareDatabase(t).Repository()
	ctx := context.Background()
	now := time.Now().Unix()
	rows := []models.ShareRecord{
		{ID: "active-old", CreatedAt: 1, ExpiresAt: now + 60},
		{ID: "revoked", CreatedAt: 2, ExpiresAt: now + 60, RevokedAt: now - 1},
		{ID: "expired", CreatedAt: 3, ExpiresAt: now},
		{ID: "active-new", CreatedAt: 4, ExpiresAt: now + 60},
	}
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error {
		for _, row := range rows {
			row.TokenHash = Hash(row.ID)
			row.SnapshotJSON = []byte(`{"entries":[],"media":[]}`)
			require.NoError(t, repo.Share.Create(ctx, &row))
			require.NoError(t, repo.Share.Update(ctx, &row))
		}
		return nil
	}))
	active, inactive := true, false
	for _, tc := range []struct {
		name   string
		active *bool
		limit  int
		offset int
		ids    []string
	}{
		{"all", nil, 10, 0, []string{"active-new", "expired", "revoked", "active-old"}},
		{"active", &active, 10, 0, []string{"active-new", "active-old"}},
		{"inactive", &inactive, 10, 0, []string{"expired", "revoked"}},
		{"second active", &active, 1, 1, []string{"active-old"}},
		{"second inactive", &inactive, 1, 1, []string{"revoked"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, repo.WithReadTxn(ctx, func(ctx context.Context) error {
				rows, err := repo.Share.List(ctx, models.ShareListOptions{Limit: tc.limit, Offset: tc.offset, Active: tc.active, Now: now})
				require.NoError(t, err)
				var ids []string
				for _, row := range rows {
					ids = append(ids, row.ID)
				}
				require.Equal(t, tc.ids, ids)
				return nil
			}))
		})
	}
}
