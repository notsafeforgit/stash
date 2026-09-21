package sharing

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/mocks"
	"github.com/stashapp/stash/pkg/sqlite"
	_ "github.com/stashapp/stash/pkg/sqlite/migrations"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func testShareDatabase(t *testing.T) *sqlite.Database {
	t.Helper()
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "shares.sqlite")))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func testService(t *testing.T) (*Service, *mocks.Database, *models.VideoFile, *sqlite.Database) {
	t.Helper()
	db := testShareDatabase(t)
	m := mocks.NewDatabase()
	repo := db.Repository()
	repo.Scene, repo.Image, repo.Gallery = m.Scene, m.Image, m.Gallery
	filePath := filepath.Join(t.TempDir(), "private-filename.mp4")
	require.NoError(t, os.WriteFile(filePath, []byte("original video"), 0o600))
	f := &models.VideoFile{BaseFile: &models.BaseFile{ID: 42, Path: filePath}, Width: 640, Height: 360, Duration: 60, FrameRate: 30}
	scene := &models.Scene{ID: 1, Title: "Private title", Files: models.NewRelatedVideoFiles([]*models.VideoFile{f})}
	m.Scene.On("Find", mock.Anything, 1).Return(scene, nil)
	return New(repo), m, f, db
}

func createTestShare(t *testing.T, s *Service) (*models.ShareRecord, string) {
	t.Helper()
	row, secret, err := s.Create(context.Background(), "owner", Options{Label: "Selected media", ExpiresAt: time.Now().Add(time.Hour)}, []Target{{Kind: "SCENE", ID: 1}})
	require.NoError(t, err)
	return row, secret
}

func TestShareCapabilitiesAndRevocation(t *testing.T) {
	s, m, _, _ := testService(t)
	defer m.AssertExpectations(t)
	ctx := context.Background()
	row, secret := createTestShare(t, s)
	other, otherSecret := createTestShare(t, s)
	require.Len(t, row.ID, 22)
	require.Len(t, secret, 43)
	require.Len(t, row.TokenHash, 32)
	require.Equal(t, Hash(secret), row.TokenHash)
	require.NotContains(t, string(row.SnapshotJSON), secret)
	_, err := s.Authenticate(ctx, row.ID, secret)
	require.ErrorIs(t, err, ErrUnavailable, "bootstrap secrets are not media credentials")
	_, _, err = s.Exchange(ctx, row.ID, otherSecret)
	require.ErrorIs(t, err, ErrUnavailable)
	session, _, err := s.Exchange(ctx, row.ID, secret)
	require.NoError(t, err)
	_, err = s.Authenticate(ctx, other.ID, session)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = s.Authenticate(ctx, row.ID, session)
	require.NoError(t, err)
	delivery, done, err := s.BeginDelivery(ctx, row)
	require.NoError(t, err)
	defer done()
	require.NoError(t, s.Revoke(ctx, row.ID))
	require.ErrorIs(t, delivery.Err(), context.Canceled)
	_, err = s.Authenticate(ctx, row.ID, session)
	require.ErrorIs(t, err, ErrUnavailable)
	_, _, err = s.Exchange(ctx, row.ID, secret)
	require.ErrorIs(t, err, ErrUnavailable)
	saved, err := s.Find(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, 1, saved.AccessCount)
	require.NotZero(t, saved.LastAccessedAt)
}

func TestShareRotationPreviewAndExpiry(t *testing.T) {
	s, m, _, _ := testService(t)
	defer m.AssertExpectations(t)
	ctx := context.Background()
	row, secret := createTestShare(t, s)
	preview, err := s.Preview(ctx, row.ID)
	require.NoError(t, err)
	previewSession, _, err := s.Exchange(ctx, row.ID, preview)
	require.NoError(t, err)
	_, _, err = s.Exchange(ctx, row.ID, preview)
	require.ErrorIs(t, err, ErrUnavailable, "preview bootstrap can only be used once")
	saved, err := s.Find(ctx, row.ID)
	require.NoError(t, err)
	require.Zero(t, saved.AccessCount)
	require.NoError(t, s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		v, err := s.Repo.Share.FindSession(ctx, Hash(previewSession))
		require.NoError(t, err)
		require.True(t, v.Preview)
		require.LessOrEqual(t, v.ExpiresAt, time.Now().Add(15*time.Minute).Unix())
		return nil
	}))
	newSecret, err := s.Rotate(ctx, row.ID)
	require.NoError(t, err)
	_, err = s.Authenticate(ctx, row.ID, previewSession)
	require.ErrorIs(t, err, ErrUnavailable)
	_, _, err = s.Exchange(ctx, row.ID, secret)
	require.ErrorIs(t, err, ErrUnavailable)
	session, _, err := s.Exchange(ctx, row.ID, newSecret)
	require.NoError(t, err)
	require.NoError(t, s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		current, err := s.Repo.Share.Find(ctx, row.ID)
		require.NoError(t, err)
		current.ExpiresAt = time.Now().Unix()
		return s.Repo.Share.Update(ctx, current)
	}))
	_, err = s.Authenticate(ctx, row.ID, session)
	require.ErrorIs(t, err, ErrUnavailable, "expiry is checked at request time, independent of cleanup")
	_, _, err = s.Exchange(ctx, row.ID, newSecret)
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestShareFrozenMembershipAndFileIdentity(t *testing.T) {
	s, m, f, _ := testService(t)
	ctx := context.Background()
	imageFile := &models.ImageFile{BaseFile: f.BaseFile, Width: 400, Height: 300}
	image := &models.Image{ID: 2, Title: "Shared image", Files: models.NewRelatedFiles([]models.File{imageFile})}
	m.Image.On("Find", mock.Anything, 2).Return(image, nil)
	m.Gallery.On("Find", mock.Anything, 7).Return(&models.Gallery{ID: 7, Title: "Gallery"}, nil).Once()
	m.Image.On("FindByGalleryID", mock.Anything, 7).Return([]*models.Image{image}, nil).Once()
	row, _, err := s.Create(ctx, "owner", Options{Label: "Mixed", ExpiresAt: time.Now().Add(time.Hour)}, []Target{{Kind: "GALLERY", ID: 7}, {Kind: "IMAGE", ID: 2}, {Kind: "SCENE", ID: 1}})
	require.NoError(t, err)
	snapshot, err := Snapshot(row)
	require.NoError(t, err)
	require.Len(t, snapshot.Entries, 3)
	require.Len(t, snapshot.Media, 2, "overlapping selections are deduplicated")
	require.Equal(t, []string{"image-2"}, snapshot.Entries[0].MediaKeys)
	require.NotContains(t, string(row.SnapshotJSON), f.Path)
	for _, item := range snapshot.Media {
		_, _, err := s.Resolve(ctx, item)
		require.NoError(t, err)
	}
	// Future gallery changes need no query: reads use only the frozen selection.
	image.Title = "Changed private title"
	updated, err := s.Update(ctx, row.ID, Options{Label: "Renamed", ExpiresAt: time.Now().Add(2 * time.Hour), AllowDownload: true, ShowMetadata: true})
	require.NoError(t, err)
	require.JSONEq(t, string(row.SnapshotJSON), string(updated.SnapshotJSON))
	require.NoError(t, os.WriteFile(f.Path, []byte("replacement file with another length"), 0o600))
	for _, item := range snapshot.Media {
		_, _, err := s.Resolve(ctx, item)
		require.ErrorIs(t, err, ErrUnavailable)
	}
	m.AssertExpectations(t)
}

func TestSharePersistenceAndValidation(t *testing.T) {
	s, _, _, db := testService(t)
	ctx := context.Background()
	row, secret := createTestShare(t, s)
	session, _, err := s.Exchange(ctx, row.ID, secret)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	// Reopen the same database using the configured path, with additive fork tables.
	require.NoError(t, db.Open(db.DatabasePath()))
	_, err = s.Authenticate(ctx, row.ID, session)
	require.NoError(t, err)
	for _, expiry := range []time.Time{time.Now().Add(-time.Second), time.Now().Add(31 * 24 * time.Hour)} {
		_, _, err = s.Create(ctx, "owner", Options{Label: "Invalid", ExpiresAt: expiry}, []Target{{Kind: "SCENE", ID: 1}})
		require.Error(t, err)
	}
	_, _, err = s.Create(ctx, "owner", Options{Label: "Invalid scope", ExpiresAt: time.Now().Add(time.Hour)}, []Target{{Kind: "PERFORMER", ID: 1}})
	require.Error(t, err)
	var snapshot map[string]any
	require.NoError(t, json.Unmarshal(row.SnapshotJSON, &snapshot))
	require.NotEmpty(t, snapshot)
}
