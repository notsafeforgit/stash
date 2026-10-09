package ingest_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	_ "github.com/stashapp/stash/pkg/sqlite/migrations"
	"github.com/stretchr/testify/require"
)

type captureFixture struct {
	db         *sqlite.Database
	service    *ingest.Service
	producer   *models.IngestProducer
	collection *models.SourceCollection
	credential *models.IngestCredential
	token      string
}

func newCaptureFixture(t *testing.T) captureFixture {
	t.Helper()
	return newCaptureFixtureForNamespace(t, "native:reddit")
}

func newCaptureFixtureForNamespace(t *testing.T, namespace string) captureFixture {
	t.Helper()
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "ingest.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	service := ingest.New(db.Repository())
	fixture := captureFixture{db: db, service: service}
	require.NoError(t, service.Repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		fixture.producer, err = service.Repo.Ingest.CreateProducer(ctx, "Gallery-dl worker")
		require.NoError(t, err)
		fixture.collection, err = service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "A feed", Kind: "feed", Namespace: namespace, State: "active"}, Origin: "review"})
		return err
	}))
	var err error
	fixture.credential, fixture.token, err = service.IssueCredential(context.Background(), fixture.producer.UUID, []models.IngestScope{{CollectionUUID: fixture.collection.UUID}}, nil)
	require.NoError(t, err)
	return fixture
}

func (f captureFixture) event(t *testing.T) ingest.CaptureEvent {
	t.Helper()
	raw, err := archive.RetainSourcePayload([]byte(`{"category":"reddit","id":"post1","title":"Source album","author":"Example","author_fullname":"t2_123","is_gallery":true,"gallery_data":{"items":[{"media_id":"image1"},{"media_id":"image2"}]},"media_metadata":{"image1":{"e":"Image"},"image2":{"e":"Image"}}}`))
	require.NoError(t, err)
	title := "Source album"
	return ingest.CaptureEvent{Protocol: 1, ProducerUUID: f.producer.UUID, EventUUID: uuid.NewString(), RunUUID: uuid.NewString(), CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, Kind: "source.capture", ObservedAt: time.Now().UTC(), ExtractorVersion: "test-1", RetentionPolicy: archive.SourceRetentionVersion, Post: ingest.PostReference{Namespace: "native:reddit", Value: "post1"}, Metadata: models.SourcePostMetadata{Title: &title}, Source: raw}
}

func eventBytes(t *testing.T, event ingest.CaptureEvent) []byte {
	t.Helper()
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	return raw
}
func (f captureFixture) submit(t *testing.T, event ingest.CaptureEvent) (*models.IngestReceipt, error) {
	t.Helper()
	raw := eventBytes(t, event)
	return f.service.Capture(context.Background(), f.token, raw, ingest.Digest(raw))
}

func TestCaptureTwitterThreadReplayConflictAndReceiptRollback(t *testing.T) {
	f := newCaptureFixtureForNamespace(t, "native:twitter")
	event := f.event(t)
	event.Post = ingest.PostReference{Namespace: "native:twitter", Value: "103"}
	event.Source = []byte(`{"category":"twitter","tweet_id":"103","conversation_id":"100","reply_id":"102","reply_user_id":"99","author":{"id":"99","name":"example"}}`)
	store := f.service.Repo.Ingest
	f.service.Repo.Ingest = failingReceiptStore{store}
	_, err := f.submit(t, event)
	require.ErrorContains(t, err, "receipt storage failed")
	f.service.Repo.Ingest = store
	first, err := f.submit(t, event)
	require.NoError(t, err)
	replay, err := f.submit(t, event)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	read := func() *models.SourceThreadView {
		var view *models.SourceThreadView
		require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			view, err = f.service.Repo.SourceThread.Read(ctx, first.PostUUID, "", 25)
			return err
		}))
		return view
	}
	view := read()
	require.False(t, view.Conflict)
	require.Equal(t, "102", view.Parent.SourceID)
	require.Len(t, view.Posts, 1)
	event.EventUUID = uuid.NewString()
	event.Source = []byte(strings.Replace(string(event.Source), `"reply_id":"102"`, `"reply_id":"101"`, 1))
	conflict, err := f.submit(t, event)
	require.NoError(t, err)
	var result ingest.CaptureResult
	require.NoError(t, json.Unmarshal(conflict.Result, &result))
	require.Contains(t, result.Review, "thread_relationship")
	view = read()
	require.True(t, view.Conflict)
	require.Equal(t, "102", view.Parent.SourceID, "conflicting captures never rewrite recorded ancestry")
}

func TestCaptureIntakeCommitsEvidenceAlbumAndStableReceipt(t *testing.T) {
	f := newCaptureFixture(t)
	event := f.event(t)
	first, err := f.submit(t, event)
	require.NoError(t, err)
	var result ingest.CaptureResult
	require.NoError(t, json.Unmarshal(first.Result, &result))
	require.Equal(t, "committed", result.Status)
	require.Equal(t, "linked", result.Publisher)
	require.Equal(t, "selected", result.Album)
	require.False(t, result.MediaIngested)
	require.NoError(t, f.service.Repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		selection, err := f.service.Repo.SourceAttachment.Selection(ctx, first.PostUUID)
		require.NoError(t, err)
		require.Len(t, selection.Entries, 2)
		require.Equal(t, "image1", selection.Entries[0].Attachment.Reference.Value)
		require.True(t, selection.Complete)
		account, err := f.service.Repo.CapturePublisher.Current(ctx, first.CaptureUUID)
		require.NoError(t, err)
		require.NotNil(t, account.AccountUUID)
		ownership, err := f.service.Repo.SourceAccount.Ownership(ctx, *account.AccountUUID)
		require.NoError(t, err)
		require.Nil(t, ownership, "publisher does not assign a depicted performer")
		association, err := f.service.Repo.SourceGallery.Association(ctx, first.PostUUID)
		require.NoError(t, err)
		require.Nil(t, association, "metadata receipt does not claim downloaded media")
		return nil
	}))
	// Concurrent duplicate deliveries serialize to exactly the same receipt.
	var wait sync.WaitGroup
	for range 4 {
		wait.Go(func() { replay, err := f.submit(t, event); require.NoError(t, err); require.Equal(t, first, replay) })
	}
	wait.Wait()
	changed := event
	changed.ExtractorVersion = "different"
	_, err = f.submit(t, changed)
	require.ErrorIs(t, err, models.ErrIngestReplay)
	changed.Kind = "future.kind"
	_, err = f.submit(t, changed)
	require.ErrorIs(t, err, models.ErrIngestReplay, "existing event identity wins over current kind validation")
	// The receipt survives server restart and a later collection edit.
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.NoError(t, f.service.Repo.WithTxn(context.Background(), func(ctx context.Context) error {
		definition := f.collection.SourceCollectionDefinition
		definition.State = "disabled"
		_, err := f.service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, SourceCollectionDefinition: definition, Origin: "review"})
		return err
	}))
	replay, err := f.submit(t, event)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	event.EventUUID = uuid.NewString()
	_, err = f.submit(t, event)
	require.ErrorIs(t, err, ingest.ErrDefinition)
}

func TestCaptureIntakeRejectsInvalidAndUnsupportedEvidenceWithoutWrites(t *testing.T) {
	f := newCaptureFixture(t)
	for _, tc := range []struct {
		name     string
		change   func(*ingest.CaptureEvent)
		expected error
	}{
		{"protocol", func(e *ingest.CaptureEvent) { e.Protocol = 2 }, ingest.ErrUnsupported},
		{"kind", func(e *ingest.CaptureEvent) { e.Kind = "file.complete" }, ingest.ErrUnsupported},
		{"post identity", func(e *ingest.CaptureEvent) { e.Post.Value = "some-other-post" }, ingest.ErrInvalid},
		{"run identity", func(e *ingest.CaptureEvent) { e.RunUUID = "not-a-uuid" }, ingest.ErrInvalid},
		{"source secrets", func(e *ingest.CaptureEvent) {
			e.Source = []byte(`{"category":"reddit","id":"post1","access_token":"secret"}`)
		}, ingest.ErrInvalid},
		{"duplicate JSON", func(e *ingest.CaptureEvent) { e.Source = []byte(`{"category":"reddit","id":"post1","id":"post2"}`) }, ingest.ErrInvalid},
		{"legacy policy", func(e *ingest.CaptureEvent) { e.RetentionPolicy = "legacy-retained-v1" }, ingest.ErrUnsupported},
		{"unsupported identity", func(e *ingest.CaptureEvent) { e.Source = []byte(`{"category":"unknown","id":"post1"}`) }, ingest.ErrUnsupported},
		{"invalid album", func(e *ingest.CaptureEvent) {
			e.Source = []byte(`{"category":"reddit","id":"post1","gallery_data":{"items":"not-a-list"}}`)
		}, ingest.ErrInvalid},
		{"another producer", func(e *ingest.CaptureEvent) { e.ProducerUUID = uuid.NewString() }, ingest.ErrForbidden},
		{"another collection", func(e *ingest.CaptureEvent) { e.CollectionUUID = uuid.NewString() }, ingest.ErrForbidden},
		{"changed root", func(e *ingest.CaptureEvent) { id := uuid.NewString(); e.RootUUID = &id }, ingest.ErrForbidden},
		{"stale collection", func(e *ingest.CaptureEvent) { e.CollectionRevision++ }, ingest.ErrDefinition},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := f.event(t)
			tc.change(&event)
			_, err := f.submit(t, event)
			require.ErrorIs(t, err, tc.expected)
		})
	}
	raw := eventBytes(t, f.event(t))
	_, err := f.service.Capture(context.Background(), f.token, raw, strings.Repeat("0", 64))
	require.ErrorIs(t, err, ingest.ErrInvalid)
	require.NoError(t, f.service.Repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		post, err := f.service.Repo.SourceEvidence.FindPostByIdentifier(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "post1"})
		require.NoError(t, err)
		require.Nil(t, post)
		return nil
	}))
}

type failingReceiptStore struct{ models.IngestReaderWriter }

func (f failingReceiptStore) RecordReceipt(context.Context, models.IngestReceipt) (*models.IngestReceipt, error) {
	return nil, errors.New("receipt storage failed")
}

func TestCaptureReceiptFailureRollsBackAllDomainWrites(t *testing.T) {
	f := newCaptureFixture(t)
	event := f.event(t)
	store := f.service.Repo.Ingest
	f.service.Repo.Ingest = failingReceiptStore{store}
	_, err := f.submit(t, event)
	require.ErrorContains(t, err, "receipt storage failed")
	f.service.Repo.Ingest = store
	require.NoError(t, f.service.Repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		post, err := f.service.Repo.SourceEvidence.FindPostByIdentifier(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "post1"})
		require.NoError(t, err)
		require.Nil(t, post)
		receipt, err := store.FindReceipt(ctx, event.ProducerUUID, event.EventUUID)
		require.NoError(t, err)
		require.Nil(t, receipt)
		return nil
	}))
	_, err = f.submit(t, event)
	require.NoError(t, err)
}

func TestProducerCredentialRotationRevocationAndReceiptScope(t *testing.T) {
	f := newCaptureFixture(t)
	event := f.event(t)
	receipt, err := f.submit(t, event)
	require.NoError(t, err)
	for _, token := range []string{"", f.token + "extra", strings.Replace(f.token, "ingest_", "", 1), f.token[:len(f.token)-2] + "xx"} {
		_, err := f.service.Authenticate(context.Background(), token)
		require.ErrorIs(t, err, ingest.ErrUnauthorized)
	}
	_, rotated, err := f.service.IssueCredential(context.Background(), f.producer.UUID, f.credential.Scopes, nil)
	require.NoError(t, err)
	require.NoError(t, f.service.Repo.WithTxn(context.Background(), func(ctx context.Context) error { return f.service.Repo.Ingest.RevokeCredential(ctx, f.credential.UUID) }))
	_, err = f.service.Authenticate(context.Background(), f.token)
	require.ErrorIs(t, err, ingest.ErrUnauthorized)
	read, err := f.service.Receipt(context.Background(), rotated, event.EventUUID)
	require.NoError(t, err)
	require.Equal(t, receipt, read)
	credential, err := f.service.Authenticate(context.Background(), rotated)
	require.NoError(t, err)
	encoded, err := json.Marshal(credential)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), credential.SecretHash)
	require.NotContains(t, string(encoded), rotated)
	var other *models.IngestProducer
	require.NoError(t, f.service.Repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		other, err = f.service.Repo.Ingest.CreateProducer(ctx, "Other worker")
		return err
	}))
	_, otherToken, err := f.service.IssueCredential(context.Background(), other.UUID, f.credential.Scopes, nil)
	require.NoError(t, err)
	_, err = f.service.Receipt(context.Background(), otherToken, event.EventUUID)
	require.ErrorIs(t, err, ingest.ErrNotFound)
}

func TestCaptureOutboxHistoricalDefinitionAlbumConflictsAndProtectedSelection(t *testing.T) {
	f := newCaptureFixture(t)
	firstEvent := f.event(t)
	// The worker captured this event before a target edit, and delivers it later.
	require.NoError(t, f.service.Repo.WithTxn(context.Background(), func(ctx context.Context) error {
		definition := f.collection.SourceCollectionDefinition
		definition.Label = "Edited feed"
		_, err := f.service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: 1, SourceCollectionDefinition: definition, Origin: "review"})
		return err
	}))
	first, err := f.submit(t, firstEvent)
	require.NoError(t, err)
	require.Equal(t, 1, first.CollectionRevision)
	second := f.event(t)
	second.Source = bytesReplace(second.Source, `"image2"`, `"different-image"`)
	conflict, err := f.submit(t, second)
	require.NoError(t, err)
	var result ingest.CaptureResult
	require.NoError(t, json.Unmarshal(conflict.Result, &result))
	require.Equal(t, "review", result.Album)
	require.Contains(t, result.Review, "album_membership")
	require.NoError(t, f.service.Repo.WithTxn(context.Background(), func(ctx context.Context) error {
		post, err := f.service.Repo.SourceEvidence.FindPost(ctx, first.PostUUID)
		require.NoError(t, err)
		_, err = f.service.Repo.SourceAttachment.DecideSelection(ctx, models.AttachmentSelectionInput{PostUUID: first.PostUUID, ExpectedPostRevision: post.Revision, CaptureUUID: first.CaptureUUID, Mode: "pinned", Origin: "review"})
		return err
	}))
	third := f.event(t)
	thirdReceipt, err := f.submit(t, third)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(thirdReceipt.Result, &result))
	require.Equal(t, "protected", result.Album)
	require.NoError(t, f.service.Repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		selection, err := f.service.Repo.SourceAttachment.Selection(ctx, first.PostUUID)
		require.NoError(t, err)
		require.Equal(t, "pinned", selection.Decision.Mode)
		require.Equal(t, "image2", selection.Entries[1].Attachment.Reference.Value)
		return nil
	}))
}

func bytesReplace(raw []byte, from, to string) []byte {
	return []byte(strings.ReplaceAll(string(raw), from, to))
}

func TestProducerExpiryAndRootScope(t *testing.T) {
	f := newCaptureFixture(t)
	expires := time.Now().Add(time.Second)
	credential, token, err := f.service.IssueCredential(context.Background(), f.producer.UUID, f.credential.Scopes, &expires)
	require.NoError(t, err)
	require.True(t, expires.Equal(*credential.ExpiresAt))
	_, err = f.service.Authenticate(context.Background(), token)
	require.NoError(t, err)
	time.Sleep(time.Until(expires.Add(time.Millisecond)))
	_, err = f.service.Authenticate(context.Background(), token)
	require.ErrorIs(t, err, ingest.ErrUnauthorized)
	var root *models.MediaRoot
	require.NoError(t, f.service.Repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		root, err = f.service.Repo.MediaRoot.Put(ctx, models.MediaRootInput{MediaRootDefinition: models.MediaRootDefinition{Label: "Logical root", State: "active"}, Origin: "review"})
		return err
	}))
	_, _, err = f.service.IssueCredential(context.Background(), f.producer.UUID, []models.IngestScope{{CollectionUUID: f.collection.UUID, RootUUID: &root.UUID}}, nil)
	require.ErrorIs(t, err, models.ErrSourceDefinitionConflict)
	require.NoError(t, f.service.Repo.WithTxn(context.Background(), func(ctx context.Context) error {
		definition := f.collection.SourceCollectionDefinition
		definition.RootUUID = &root.UUID
		definition.PathPrefix = "."
		_, err := f.service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: 1, SourceCollectionDefinition: definition, Origin: "review"})
		return err
	}))
	event := f.event(t)
	event.CollectionRevision = 2
	event.RootUUID = &root.UUID
	_, err = f.submit(t, event)
	require.ErrorIs(t, err, ingest.ErrForbidden, "metadata-only grant does not become filesystem root permission")
	_, rootToken, err := f.service.IssueCredential(context.Background(), f.producer.UUID, []models.IngestScope{{CollectionUUID: f.collection.UUID, RootUUID: &root.UUID}}, nil)
	require.NoError(t, err)
	raw := eventBytes(t, event)
	_, err = f.service.Capture(context.Background(), rootToken, raw, ingest.Digest(raw))
	require.NoError(t, err)
	_, _, err = f.service.IssueCredential(context.Background(), f.producer.UUID, f.credential.Scopes, nil)
	require.NoError(t, err, "rotation can retain an explicitly requested historical root grant")
}

func TestCaptureContentRepeatDoesNotDuplicateEvidenceOrReplayCount(t *testing.T) {
	f := newCaptureFixture(t)
	event := f.event(t)
	first, err := f.submit(t, event)
	require.NoError(t, err)
	repeated := event
	repeated.EventUUID = uuid.NewString()
	repeated.ObservedAt = event.ObservedAt.Add(time.Hour)
	var object map[string]any
	require.NoError(t, json.Unmarshal(event.Source, &object))
	object["score"] = 42
	object["search_tags"] = "author:Example"
	repeated.Source, err = json.Marshal(object)
	require.NoError(t, err)
	second, err := f.submit(t, repeated)
	require.NoError(t, err)
	require.Equal(t, first.CaptureUUID, second.CaptureUUID)
	replay, err := f.submit(t, repeated)
	require.NoError(t, err)
	require.Equal(t, second, replay)
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		history, err := f.service.Repo.SourceEvidence.CaptureHistory(ctx, first.PostUUID, "", 25)
		require.NoError(t, err)
		require.Len(t, history, 1)
		require.Equal(t, 2, history[0].Count)
		require.Equal(t, repeated.ObservedAt, *history[0].LastSeen)
		selection, err := f.service.Repo.SourceAttachment.Selection(ctx, first.PostUUID)
		require.NoError(t, err)
		require.Len(t, selection.Entries, 2)
		publisher, err := f.service.Repo.CapturePublisher.Current(ctx, first.CaptureUUID)
		require.NoError(t, err)
		require.NotNil(t, publisher.AccountUUID)
		return nil
	}))
	// A text edit, followed by a real reversion, must still advance current
	// evidence even though the reverted post body already exists.
	edited := repeated
	edited.EventUUID = uuid.NewString()
	edited.ObservedAt = event.ObservedAt.Add(2 * time.Hour)
	title := "Edited post"
	edited.Metadata.Title = &title
	object["title"] = title
	edited.Source, err = json.Marshal(object)
	require.NoError(t, err)
	third, err := f.submit(t, edited)
	require.NoError(t, err)
	require.NotEqual(t, first.CaptureUUID, third.CaptureUUID)
	reverted := event
	reverted.EventUUID = uuid.NewString()
	reverted.ObservedAt = event.ObservedAt.Add(3 * time.Hour)
	fourth, err := f.submit(t, reverted)
	require.NoError(t, err)
	require.NotEqual(t, first.CaptureUUID, fourth.CaptureUUID)
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		history, err := f.service.Repo.SourceEvidence.CaptureHistory(ctx, first.PostUUID, "", 25)
		require.NoError(t, err)
		require.Len(t, history, 2)
		captures, err := f.service.Repo.SourceEvidence.CurrentCaptures(ctx, first.PostUUID, nil, 25)
		require.NoError(t, err)
		require.Len(t, captures, 3)
		require.Equal(t, fourth.CaptureUUID, captures[2].UUID)
		return nil
	}))
}
