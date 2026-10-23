package sharing

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestShareDeleteInactiveAcrossPages(t *testing.T) {
	s, _, file, _ := testService(t)
	ctx := t.Context()
	active, activeSecret := createTestShare(t, s)
	renewed, renewedSecret := createTestShare(t, s)
	expired, expiredSecret := createTestShare(t, s)
	revoked, revokedSecret := createTestShare(t, s)
	activeSession, _, err := s.Exchange(ctx, active.ID, activeSecret)
	require.NoError(t, err)
	expiredSession, _, err := s.Exchange(ctx, expired.ID, expiredSecret)
	require.NoError(t, err)
	secondSession, _, err := s.Exchange(ctx, expired.ID, expiredSecret)
	require.NoError(t, err)
	activeDelivery, finishActive, err := s.BeginDelivery(ctx, active)
	require.NoError(t, err)
	defer finishActive()
	expiredDelivery, finishExpired, err := s.BeginDelivery(ctx, expired)
	require.NoError(t, err)
	defer finishExpired()
	require.NoError(t, s.Revoke(ctx, revoked.ID))
	expected := []string{expired.ID, revoked.ID}
	require.NoError(t, s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		for _, row := range []*models.ShareRecord{expired, renewed} {
			row.ExpiresAt = time.Now().Unix()
			if err := s.Repo.Share.Update(ctx, row); err != nil {
				return err
			}
		}
		// More than the list API's maximum page, all sharing the same media.
		for i := range 120 {
			row := *expired
			row.ID = fmt.Sprintf("obsolete-%03d", i)
			row.TokenHash = Hash(row.ID)
			if err := s.Repo.Share.Create(ctx, &row); err != nil {
				return err
			}
			expected = append(expected, row.ID)
		}
		return nil
	}))
	_, err = s.Update(ctx, renewed.ID, Options{Label: "Renewed", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	var invalidated []string
	s.OnInvalidate = func(id string) { invalidated = append(invalidated, id) }
	count, err := s.DeleteInactive(ctx)
	require.NoError(t, err)
	require.Equal(t, 122, count)
	require.ElementsMatch(t, expected, invalidated)
	require.ErrorIs(t, expiredDelivery.Err(), context.Canceled)
	require.NoError(t, activeDelivery.Err())
	require.NoError(t, s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		rows, err := s.Repo.Share.List(ctx, models.ShareListOptions{Limit: 200})
		require.NoError(t, err)
		require.Len(t, rows, 2)
		require.ElementsMatch(t, []string{active.ID, renewed.ID}, []string{rows[0].ID, rows[1].ID})
		for _, token := range []string{expiredSession, secondSession} {
			session, err := s.Repo.Share.FindSession(ctx, Hash(token))
			require.NoError(t, err)
			require.Nil(t, session)
		}
		return nil
	}))
	_, err = s.Authenticate(ctx, active.ID, activeSession)
	require.NoError(t, err)
	_, _, err = s.Exchange(ctx, renewed.ID, renewedSecret)
	require.NoError(t, err)
	for _, link := range []struct{ id, secret string }{{expired.ID, expiredSecret}, {revoked.ID, revokedSecret}} {
		_, _, err = s.Exchange(ctx, link.id, link.secret)
		require.ErrorIs(t, err, ErrUnavailable)
	}
	count, err = s.DeleteInactive(ctx)
	require.NoError(t, err)
	require.Zero(t, count, "repeated cleanup is harmless")
	require.Len(t, invalidated, 122, "active shares are never invalidated")
	require.FileExists(t, file.Path)
}

type failInactiveDeleteStore struct {
	models.ShareReaderWriter
	failure error
}

func (s failInactiveDeleteStore) DeleteInactive(ctx context.Context, now int64) ([]string, error) {
	ids, err := s.ShareReaderWriter.DeleteInactive(ctx, now)
	if err != nil {
		return nil, err
	}
	return ids, s.failure
}

func TestShareDeleteInactiveRollsBackBeforeCancellation(t *testing.T) {
	s, _, file, _ := testService(t)
	ctx := t.Context()
	row, secret := createTestShare(t, s)
	session, _, err := s.Exchange(ctx, row.ID, secret)
	require.NoError(t, err)
	delivery, done, err := s.BeginDelivery(ctx, row)
	require.NoError(t, err)
	defer done()
	require.NoError(t, s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		row.ExpiresAt = time.Now().Unix()
		return s.Repo.Share.Update(ctx, row)
	}))
	store := s.Repo.Share
	failure := errors.New("deletion interrupted")
	s.Repo.Share = failInactiveDeleteStore{store, failure}
	var invalidated []string
	s.OnInvalidate = func(id string) { invalidated = append(invalidated, id) }
	count, err := s.DeleteInactive(ctx)
	require.ErrorIs(t, err, failure)
	require.Zero(t, count)
	require.Empty(t, invalidated)
	require.NoError(t, delivery.Err())
	require.NoError(t, s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		saved, err := store.Find(ctx, row.ID)
		require.NoError(t, err)
		require.NotNil(t, saved)
		guest, err := store.FindSession(ctx, Hash(session))
		require.NoError(t, err)
		require.NotNil(t, guest, "cascaded session deletion also rolls back")
		return nil
	}))
	s.Repo.Share = store
	count, err = s.DeleteInactive(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, []string{row.ID}, invalidated)
	require.ErrorIs(t, delivery.Err(), context.Canceled)
	require.FileExists(t, file.Path)
}

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
			secondSession, _, err := s.Exchange(ctx, row.ID, secret)
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
				for _, token := range []string{session, secondSession} {
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
