package ingest_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func profileObservationFixture(t *testing.T) (captureFixture, ingest.AccountProfileEvent) {
	t.Helper()
	f := newIntakePublicationFixture(t, false)
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		d := f.collection.SourceCollectionDefinition
		d.Kind, d.Namespace, d.TargetURL = "account", "native:reddit", "https://www.reddit.com/user/example/submitted/?sort=new"
		var err error
		f.collection, err = f.service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, SourceCollectionDefinition: d, Origin: "review"})
		return err
	}))
	var err error
	f.credential, f.token, err = f.service.IssueCredential(t.Context(), f.producer.UUID, []models.IngestScope{{CollectionUUID: f.collection.UUID, RootUUID: &f.root.UUID}}, nil)
	require.NoError(t, err)
	coordinator := ingest.NewRunCoordinator(f.service)
	now := time.Now().UTC().Truncate(time.Millisecond)
	coordinator.Now = func() time.Time { return now }
	policy := strings.Repeat("a", 64)
	run, err := coordinator.Submit(t.Context(), f.token, models.SourceRunRequest{RequestUUID: uuid.NewString(), CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, Operation: "download", PolicySHA256: policy, Window: models.SourceWindow{Until: now}})
	require.NoError(t, err)
	run, err = coordinator.Claim(t.Context(), f.token, run.UUID, uuid.NewString(), policy, time.Minute)
	require.NoError(t, err)
	event := ingest.AccountProfileEvent{Protocol: 1, ProducerUUID: f.producer.UUID, EventUUID: uuid.NewString(), RunUUID: run.UUID, CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, RootUUID: &f.root.UUID, OwnerUUID: run.OwnerUUID, Fence: run.Fence, Kind: "account.profile", ObservedAt: time.Now().UTC(), ExtractorVersion: "fixture", RetentionPolicy: archive.SourceRetentionVersion,
		Source: json.RawMessage(`{"category":"reddit","user":{"id":"123","name":"Example","subreddit":{"public_description":"https://bio.invalid https://onlyfans.com/Example/trial/token"},"social_links":[{"url":"https://myredlink.com/example"},{"url":"https://www.onlyfans.com/example?ref=bio"}],"resolved_links":[{"url":"https://myredlink.com/example","resolved_url":"https://onlyfans.com/example/c76"}]}}`)}
	return f.captureFixture, event
}

func submitProfile(t *testing.T, f captureFixture, event ingest.AccountProfileEvent) (*models.IngestReceipt, error) {
	t.Helper()
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	return f.service.AccountProfile(t.Context(), f.token, raw, ingest.Digest(raw))
}

func TestAccountProfileWithoutPostsReplayLateOwnerAndRemoval(t *testing.T) {
	f, event := profileObservationFixture(t)
	updates := 0
	f.service.Repo.TxnManager = models.WithEntityUpdateNotifier(f.service.Repo.TxnManager, func(context.Context, models.ArchiveEntityKind, int, []string) { updates++ })
	store := f.service.Repo.Ingest
	f.service.Repo.Ingest = failingReceiptStore{store}
	_, err := submitProfile(t, f, event)
	require.ErrorContains(t, err, "receipt storage failed")
	f.service.Repo.Ingest = store
	first, err := submitProfile(t, f, event)
	require.NoError(t, err)
	var result models.AccountProfileResult
	require.NoError(t, json.Unmarshal(first.Result, &result))
	require.NotEmpty(t, result.AccountUUID)
	require.Empty(t, first.PostUUID)
	require.Empty(t, first.CaptureUUID)
	var performerID int
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		repo := f.service.Repo
		account, err := repo.SourceAccount.Find(ctx, result.AccountUUID)
		require.NoError(t, err)
		p := models.NewPerformer()
		p.Name = "Profile owner"
		require.NoError(t, repo.Performer.Create(ctx, &models.CreatePerformerInput{Performer: &p}))
		performerID = p.ID
		entity, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchivePerformer, p.ID)
		require.NoError(t, err)
		_, err = repo.SourceAccount.DecideOwnership(ctx, models.AccountOwnershipInput{AccountUUID: account.UUID, ExpectedAccountRevision: account.Revision, State: models.AccountOwnershipLinked, PerformerUUID: entity.UUID, ExpectedPerformerRevision: entity.Revision, Origin: "review"})
		return err
	}))
	urls := func() []string {
		var urls []string
		require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			urls, err = f.service.Repo.Performer.GetURLs(ctx, performerID)
			return err
		}))
		return urls
	}
	require.ElementsMatch(t, []string{"https://bio.invalid", "https://onlyfans.com/example"}, urls())
	require.Equal(t, 1, updates)
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		update := models.NewPerformerPartial()
		update.URLs = &models.UpdateStrings{Mode: models.RelationshipUpdateModeSet, Values: []string{"https://bio.invalid"}}
		_, err := f.service.Repo.Performer.UpdatePartial(ctx, performerID, update)
		return err
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	replay, err := submitProfile(t, f, event)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	event.EventUUID = uuid.NewString()
	event.Source = json.RawMessage(strings.Replace(string(event.Source), "https://bio.invalid", "https://bio.invalid https://new.invalid", 1))
	event.Source = json.RawMessage(strings.Replace(string(event.Source), "/example/c76", "/example/c99", 1))
	_, err = submitProfile(t, f, event)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"https://bio.invalid", "https://new.invalid"}, urls())
	require.Equal(t, 2, updates)
	raw, err := sql.Open("sqlite3", f.db.DatabasePath())
	require.NoError(t, err)
	defer raw.Close()
	var count int
	for _, table := range []string{"source_posts", "source_captures"} {
		require.NoError(t, raw.QueryRow("SELECT count(*) FROM "+table).Scan(&count))
		require.Zero(t, count)
	}
	require.NoError(t, raw.QueryRow("SELECT count(*) FROM source_account_profiles").Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, raw.QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&count))
	require.Zero(t, count)
	// Ordinary export must strip standalone profiles along with post captures.
	destination := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, destination)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	anon, err := sql.Open("sqlite3", destination)
	require.NoError(t, err)
	defer anon.Close()
	for _, table := range []string{"source_account_profiles", "account_observed_profile_urls", "ingest_receipts"} {
		require.NoError(t, anon.QueryRow("SELECT count(*) FROM "+table).Scan(&count))
		require.Zero(t, count)
	}
	require.NoError(t, raw.Close())
	require.NoError(t, f.db.Close())
	_, err = sqlite.VerifyNativeSnapshot(t.Context(), f.db.DatabasePath())
	require.NoError(t, err)
	corrupt, err := sql.Open("sqlite3", f.db.DatabasePath())
	require.NoError(t, err)
	_, err = corrupt.Exec("UPDATE account_observed_profile_urls SET url_key='wrong:'||url_key")
	require.NoError(t, err)
	require.NoError(t, corrupt.Close())
	_, err = sqlite.VerifyNativeSnapshot(t.Context(), f.db.DatabasePath())
	require.ErrorIs(t, err, models.ErrSourcePayloadCorrupt)
}

func TestAccountProfileRejectsForeignIdentityAndAttempt(t *testing.T) {
	f, event := profileObservationFixture(t)
	for _, change := range []func(*ingest.AccountProfileEvent){
		func(e *ingest.AccountProfileEvent) {
			e.Source = json.RawMessage(strings.Replace(string(e.Source), "Example", "someoneelse", 1))
		},
		func(e *ingest.AccountProfileEvent) { e.OwnerUUID = uuid.NewString() },
		func(e *ingest.AccountProfileEvent) { e.Fence++ },
		func(e *ingest.AccountProfileEvent) { e.RunUUID = uuid.NewString() },
		func(e *ingest.AccountProfileEvent) {
			e.Source = json.RawMessage(`{"category":"reddit","user":{"name":"Example"}}`)
		},
	} {
		next := event
		next.EventUUID = uuid.NewString()
		change(&next)
		_, err := submitProfile(t, f, next)
		require.Error(t, err)
	}
}

func TestAccountProfileRetainsAmbiguityWithoutAssigningOwner(t *testing.T) {
	for _, test := range []struct {
		name   string
		ids    []string
		review string
	}{
		{"handle only", []string{""}, "locator_only_candidates"},
		{"duplicate stable ID", []string{"t2_123", "t2_123"}, "ambiguous_stable_id"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, event := profileObservationFixture(t)
			require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
				for _, id := range test.ids {
					a, err := f.service.Repo.SourceAccount.Create(ctx, "native:reddit", "Example")
					require.NoError(t, err)
					ref := models.AccountReference{Namespace: "native:reddit", Kind: "handle", Value: "example"}
					if id != "" {
						ref.Kind, ref.Value = "id", id
					}
					_, err = f.service.Repo.SourceAccount.ObserveIdentifier(ctx, a.UUID, ref, models.AccountIdentifierEvidence{Key: "fixture", Basis: "explicit", Origin: "review", Details: json.RawMessage(`{}`), FirstObserved: event.ObservedAt, LastObserved: event.ObservedAt})
					require.NoError(t, err)
				}
				return nil
			}))
			receipt, err := submitProfile(t, f, event)
			require.NoError(t, err)
			var result models.AccountProfileResult
			require.NoError(t, json.Unmarshal(receipt.Result, &result))
			require.Equal(t, test.review, result.Review)
			require.Empty(t, result.AccountUUID)
		})
	}
}
