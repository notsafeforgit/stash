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
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO scenes(id,title,created_at,updated_at) VALUES(1,'Private title',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO images(id,title,created_at,updated_at) VALUES(2,'Shared image',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO galleries(id,title,created_at,updated_at) VALUES(7,'Gallery',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO galleries_images(gallery_id,image_id) VALUES(7,2);`, nil)
		return err
	}))
	repo.Scene, repo.Image, repo.Gallery = m.Scene, m.Image, m.Gallery
	filePath := filepath.Join(t.TempDir(), "private-filename.mp4")
	require.NoError(t, os.WriteFile(filePath, []byte("original video"), 0o600))
	f := &models.VideoFile{BaseFile: &models.BaseFile{ID: 42, Path: filePath}, Width: 640, Height: 360, Duration: 60, FrameRate: 30}
	stat, err := os.Stat(filePath)
	require.NoError(t, err)
	f.Generation = 1
	f.Size = stat.Size()
	f.ModTime = stat.ModTime()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO folders(id,path,basename,mod_time,created_at,updated_at) VALUES(1,?,'media',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at) VALUES(42,1,?, ?, ?, CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO video_files(file_id,duration,video_codec,format,audio_codec,width,height,frame_rate,bit_rate) VALUES(42,60,'h264','mp4','aac',640,360,30,100);
INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(1,42,1);
INSERT INTO images_files(image_id,file_id,"primary") VALUES(2,42,1);`, []any{filepath.Dir(filePath), filepath.Base(filePath), stat.Size(), stat.ModTime().UTC().Format(time.RFC3339)})
		return err
	}))
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
	require.Equal(t, secret, row.Token)
	require.NotContains(t, testSnapshotJSON(t, s, row), secret)
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
	require.Empty(t, saved.Token, "revocation discards the saved key")
	require.Empty(t, SavedLinkSecret(saved))
	require.Equal(t, 1, saved.AccessCount)
	require.NotZero(t, saved.LastAccessedAt)
}

func TestShareSecretLookupPreservesIdentityAndLifecycle(t *testing.T) {
	s, mocks, _, _ := testService(t)
	defer mocks.AssertExpectations(t)
	row, secret := createTestShare(t, s)
	other, otherSecret := createTestShare(t, s)
	session, _, err := s.Exchange(t.Context(), row.ID, secret)
	require.NoError(t, err)
	for _, invalid := range []string{"", row.ID, secret + "=", secret + "\n", session} {
		_, err := s.ResolveLink(t.Context(), invalid)
		require.ErrorIs(t, err, ErrUnavailable)
	}
	resolved, err := s.ResolveLink(t.Context(), secret)
	require.NoError(t, err)
	require.Equal(t, row.ID, resolved.ID)
	require.Equal(t, 1, resolved.AccessCount, "lookup must not count a visit or issue a session")
	resolved.Token = ""
	require.NoError(t, s.Repo.WithTxn(t.Context(), func(ctx context.Context) error { return s.Repo.Share.Update(ctx, resolved) }))
	resolved, err = s.ResolveLink(t.Context(), secret)
	require.NoError(t, err, "historical hash-only links remain usable")
	require.Empty(t, resolved.Token)
	rotated, err := s.Rotate(t.Context(), row.ID)
	require.NoError(t, err)
	_, err = s.ResolveLink(t.Context(), secret)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = s.Authenticate(t.Context(), row.ID, session)
	require.ErrorIs(t, err, ErrUnavailable)
	resolved, err = s.ResolveLink(t.Context(), rotated)
	require.NoError(t, err)
	require.Equal(t, row.ID, resolved.ID)
	resolvedOther, err := s.ResolveLink(t.Context(), otherSecret)
	require.NoError(t, err)
	require.Equal(t, other.ID, resolvedOther.ID)
	resolved.ExpiresAt = time.Now().Unix() - 1
	require.NoError(t, s.Repo.WithTxn(t.Context(), func(ctx context.Context) error { return s.Repo.Share.Update(ctx, resolved) }))
	_, err = s.ResolveLink(t.Context(), rotated)
	require.ErrorIs(t, err, ErrUnavailable)
	require.NoError(t, s.Revoke(t.Context(), other.ID))
	_, err = s.ResolveLink(t.Context(), otherSecret)
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestShareRotationPreviewAndExpiry(t *testing.T) {
	s, m, _, _ := testService(t)
	defer m.AssertExpectations(t)
	ctx := context.Background()
	row, secret := createTestShare(t, s)
	// Recipient preview uses exactly the same reusable link and session rules.
	preview := SavedLinkSecret(row)
	require.Equal(t, secret, preview)
	previewSession, _, err := s.Exchange(ctx, row.ID, preview)
	require.NoError(t, err)
	_, _, err = s.Exchange(ctx, row.ID, preview)
	require.NoError(t, err, "copying a preview URL remains forwardable")
	saved, err := s.Find(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, 2, saved.AccessCount)
	require.Equal(t, secret, SavedLinkSecret(saved))
	require.NoError(t, s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		v, err := s.Repo.Share.FindSession(ctx, Hash(previewSession))
		require.NoError(t, err)
		require.Equal(t, row.ExpiresAt, v.ExpiresAt)
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
	row, _, err := s.Create(ctx, "owner", Options{Label: "Mixed", ExpiresAt: time.Now().Add(time.Hour)}, []Target{{Kind: "GALLERY", ID: 7}, {Kind: "IMAGE", ID: 2}, {Kind: "SCENE", ID: 1}})
	require.NoError(t, err)
	snapshot, err := testSnapshot(t, s, row)
	require.NoError(t, err)
	require.Len(t, snapshot.Entries, 3)
	require.Len(t, snapshot.Media, 2, "overlapping selections are deduplicated")
	require.Equal(t, snapshot.Entries[1].MediaKeys, snapshot.Entries[0].MediaKeys)
	require.NotContains(t, testSnapshotJSON(t, s, row), f.Path)
	for _, item := range snapshot.Media {
		_, _, err := s.Resolve(ctx, item)
		require.NoError(t, err)
	}
	// Future gallery changes need no query: reads use only the frozen selection.
	image.Title = "Changed private title"
	updated, err := s.Update(ctx, row.ID, Options{Label: "Renamed", ExpiresAt: time.Now().Add(2 * time.Hour), AllowDownload: true, ShowMetadata: true})
	require.NoError(t, err)
	require.JSONEq(t, testSnapshotJSON(t, s, row), testSnapshotJSON(t, s, updated))
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
	// Reopen the same native database; both the link and guest session persist.
	require.NoError(t, db.Open(db.DatabasePath()))
	saved, err := s.Find(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, secret, SavedLinkSecret(saved))
	encoded, err := json.Marshal(saved)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), secret, "raw records never serialize the saved capability")
	_, err = s.Authenticate(ctx, row.ID, session)
	require.NoError(t, err)
	for _, expiry := range []time.Time{time.Now().Add(-time.Second), time.Now().Add(31 * 24 * time.Hour)} {
		_, _, err = s.Create(ctx, "owner", Options{Label: "Invalid", ExpiresAt: expiry}, []Target{{Kind: "SCENE", ID: 1}})
		require.Error(t, err)
	}
	_, _, err = s.Create(ctx, "owner", Options{Label: "Invalid scope", ExpiresAt: time.Now().Add(time.Hour)}, []Target{{Kind: "PERFORMER", ID: 1}})
	require.Error(t, err)
	var snapshot map[string]any
	require.NoError(t, json.Unmarshal([]byte(testSnapshotJSON(t, s, row)), &snapshot))
	require.NotEmpty(t, snapshot)
}

func TestShareHashOnlyLinksRemainValidUntilExplicitRotation(t *testing.T) {
	s, m, _, _ := testService(t)
	defer m.AssertExpectations(t)
	row, secret := createTestShare(t, s)
	ctx := t.Context()
	row.Token = "" // A share created before keys were retained.
	require.NoError(t, s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		return s.Repo.Share.Update(ctx, row)
	}))
	saved, err := s.Find(ctx, row.ID)
	require.NoError(t, err)
	require.Empty(t, SavedLinkSecret(saved))
	require.Equal(t, 1, saved.Version)
	session, _, err := s.Exchange(ctx, row.ID, secret)
	require.NoError(t, err)
	_, err = s.AuthenticateLink(ctx, row.ID, secret)
	require.NoError(t, err, "existing link previews remain valid")
	updated, err := s.Update(ctx, row.ID, Options{Label: "Edited", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	require.Empty(t, SavedLinkSecret(updated), "editing cannot replace a previously sent link")
	replacement, err := s.Rotate(ctx, row.ID)
	require.NoError(t, err)
	require.NotEqual(t, secret, replacement)
	saved, err = s.Find(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, replacement, SavedLinkSecret(saved))
	require.Equal(t, 2, saved.Version)
	_, err = s.Authenticate(ctx, row.ID, session)
	require.ErrorIs(t, err, ErrUnavailable)
	_, _, err = s.Exchange(ctx, row.ID, secret)
	require.ErrorIs(t, err, ErrUnavailable)
	_, _, err = s.Exchange(ctx, row.ID, replacement)
	require.NoError(t, err)
}

func TestSavedShareLinkOnlyReturnsTheCurrentActiveCredential(t *testing.T) {
	s, m, _, _ := testService(t)
	defer m.AssertExpectations(t)
	row, secret := createTestShare(t, s)
	ctx := t.Context()
	updated, err := s.Update(ctx, row.ID, Options{Label: "Renamed", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	require.Equal(t, secret, SavedLinkSecret(updated))
	require.Equal(t, row.Version, updated.Version)
	require.Zero(t, updated.AccessCount, "retrieving the link is not a recipient visit")
	require.Empty(t, SavedLinkSecret(nil))
	for _, change := range []func(*models.ShareRecord){
		func(r *models.ShareRecord) { r.Token = "" },
		func(r *models.ShareRecord) { r.Token = "invalid" },
		func(r *models.ShareRecord) { r.TokenHash = Hash("different") },
		func(r *models.ShareRecord) { r.RevokedAt = time.Now().Unix() },
		func(r *models.ShareRecord) { r.ExpiresAt = time.Now().Unix() },
	} {
		changed := *updated
		change(&changed)
		require.Empty(t, SavedLinkSecret(&changed))
	}
}

func testSnapshot(t *testing.T, s *Service, row *models.ShareRecord) (models.ShareSnapshot, error) {
	t.Helper()
	ret, _, err := s.Content(t.Context(), row, "")
	return ret, err
}
func testSnapshotJSON(t *testing.T, s *Service, row *models.ShareRecord) string {
	t.Helper()
	value, err := testSnapshot(t, s, row)
	require.NoError(t, err)
	body, err := json.Marshal(value)
	require.NoError(t, err)
	return string(body)
}
