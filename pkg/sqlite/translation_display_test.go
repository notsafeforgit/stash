package sqlite_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/translation"
	"github.com/stretchr/testify/require"
)

func translationDisplayFixture(t *testing.T, repo models.Repository) (*models.SourceCapture, string, string, string) {
	t.Helper()
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "translation-display"}, "")
	input := sourceTestCapture(t, post.UUID, 1, "Profile")
	caption := "日本語の見出し\n元の説明"
	input.Metadata.Title, input.Metadata.OriginalText = nil, &caption
	capture := recordSourceTestCapture(t, repo, input)
	video := sourceAttachmentEntry(1, "video")
	video.MediaKind = "video"
	recordAttachmentManifest(t, repo, models.SourceAttachmentManifestInput{CaptureUUID: capture.UUID, Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "image"), video}})
	selection := applySelection(t, repo, models.AttachmentSelectionInput{PostUUID: post.UUID, ExpectedPostRevision: selectionPost(t, repo, post.UUID).Revision,
		CaptureUUID: capture.UUID, Mode: "automatic", Origin: "ingest"})
	image := chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
	scene := chooseAlbumMedia(t, repo, selection.Entries[1].Attachment.UUID, models.ArchiveScene, 31)
	for _, entity := range []string{image.UUID, scene.UUID} {
		for _, field := range []string{"title", "details"} {
			value := caption
			if field == "title" {
				value = "日本語の見出し"
			}
			raw, err := json.Marshal(value)
			require.NoError(t, err)
			input := metadataInput(metadataState(t, repo, entity, field), "inherit", "review", string(raw))
			input.CaptureUUID = capture.UUID
			_, err = applyMetadata(repo, input, false)
			require.NoError(t, err)
		}
	}
	return capture, image.UUID, scene.UUID, post.UUID
}

func backfillDisplay(t *testing.T, repo models.Repository, post string, adopt bool) []models.TranslationBackfillResult {
	t.Helper()
	var ret []models.TranslationBackfillResult
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceTranslation.Backfill(ctx, models.TranslationBackfillInput{Posts: []string{post}, TargetLanguage: "en",
			ProviderPolicy: models.TranslationBingTextV1, ApplyToLibrary: true, AdoptMatchingText: adopt}, time.Now().UTC())
		return err
	}))
	return ret
}

func drainDisplay(t *testing.T, repo models.Repository, limit int) {
	t.Helper()
	for range 20 {
		n := 0
		require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			var err error
			n, err = repo.SourceTranslation.RefreshPending(ctx, limit)
			return err
		}))
		if n == 0 {
			return
		}
	}
	t.Fatal("translation display cursor failed to finish")
}

func TestTranslationDisplayRefreshesMixedAlbumAfterRestartPreservesOriginalAndEdits(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	capture, image, scene, post := translationDisplayFixture(t, repo)
	gallery := syncSourceGallery(t, repo, post)
	_, err := applyMetadata(repo, metadataInput(metadataState(t, repo, image, "title"), "set", "review", `"My title"`), false)
	require.NoError(t, err)
	_, err = applyMetadata(repo, metadataInput(metadataState(t, repo, image, "details"), "clear", "review", ""), false)
	require.NoError(t, err)
	result := backfillDisplay(t, repo, post, false)
	require.Equal(t, "pending", result[0].Status)
	s := translation.New(repo)
	calls := 0
	worker := translation.NewWorker(s, translation.ProviderFunc(func(_ context.Context, input models.TranslationRequest) (models.TranslationCacheInput, error) {
		calls++
		require.Equal(t, *capture.Metadata.OriginalText, input.OriginalText)
		return models.TranslationCacheInput{Status: "translated", TranslatedText: translationPointer("English heading\nOriginal description"), SourceLanguage: translationPointer("ja"), Provider: translationPointer("fixture")}, nil
	}))
	processTranslation(t, worker)
	require.Equal(t, 1, calls)
	// Completion committed the display queue. Propagation survives a restore
	// without another provider call, including pages containing mixed media.
	backup := filepath.Join(t.TempDir(), "display.sqlite")
	require.NoError(t, db.Backup(backup))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(backup))
	repo = db.Repository()
	drainDisplay(t, repo, 1)
	for _, id := range []string{scene, gallery.GalleryUUID} {
		require.JSONEq(t, `"English heading"`, string(metadataState(t, repo, id, "title").Value))
		require.JSONEq(t, `"English heading\nOriginal description"`, string(metadataState(t, repo, id, "details").Value))
		require.False(t, metadataState(t, repo, id, "details").Protected)
	}
	require.JSONEq(t, `"My title"`, string(metadataState(t, repo, image, "title").Value))
	require.Equal(t, "clear", metadataState(t, repo, image, "details").Mode)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		original, err := repo.SourceEvidence.FindCapture(ctx, capture.UUID)
		require.NoError(t, err)
		require.Equal(t, capture.Metadata, original.Metadata)
		display, err := repo.SourceTranslation.Display(ctx, post, "twitter", original.Metadata)
		require.NoError(t, err)
		require.True(t, display.Translated)
		require.Equal(t, "English heading", display.Title)
		return nil
	}))
	before := metadataHistory(t, repo, scene, "details")
	require.Equal(t, "completed", backfillDisplay(t, repo, post, false)[0].Status)
	drainDisplay(t, repo, 1)
	require.Equal(t, before, metadataHistory(t, repo, scene, "details"), "replay must not accumulate identical decisions")
	sourceGalleryMemberships(t, repo, *gallery.GalleryID, []int{41}, []int{31})
}

func TestTranslationDisplayUsesCacheBeforeAlbumCreationAndAdoptsOnlyMatchingText(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	capture, image, scene, post := translationDisplayFixture(t, repo)
	// Reproduce older API-written source text without native field provenance.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.Scene.UpdatePartial(ctx, 31, models.ScenePartial{Title: models.NewOptionalString("Custom title"), Details: models.NewOptionalString(*capture.Metadata.OriginalText)})
		return err
	}))
	request := retainTranslationRequest(t, repo, *capture.Metadata.OriginalText)
	retainTranslationCache(t, repo, models.TranslationCacheInput{RequestUUID: request.UUID, Status: "translated", TranslatedText: translationPointer("Cached heading\nCached description"), SourceLanguage: translationPointer("ja"), Provider: translationPointer("fixture"), CapturedAt: time.Now().UTC().Format(time.RFC3339Nano), Origin: "worker"})
	result := backfillDisplay(t, repo, post, true)
	require.Equal(t, "completed", result[0].Status)
	preview := sourceGalleryPreview(t, repo, post)
	require.Equal(t, "Cached heading", preview.Title)
	require.Equal(t, "Cached heading\nCached description", preview.Details)
	// A user edit after the reviewed backfill must not be adopted, even when
	// its text happens to equal the source.
	_, err := applyMetadata(repo, metadataInput(metadataState(t, repo, image, "title"), "set", "review", `"日本語の見出し"`), false)
	require.NoError(t, err)
	drainDisplay(t, repo, 2)
	require.JSONEq(t, `"Custom title"`, string(metadataState(t, repo, scene, "title").Value))
	require.JSONEq(t, `"Cached heading\nCached description"`, string(metadataState(t, repo, scene, "details").Value))
	require.JSONEq(t, `"日本語の見出し"`, string(metadataState(t, repo, image, "title").Value))
}

func TestTranslationNamespaceDefaultSchedulesFutureSourcesWithoutOverridingOptOut(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	definition := translationPolicyDefinition()
	definition.ApplyToLibrary, definition.Title = true, false
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.TranslationPolicy.PutDefault(ctx, models.TranslationPolicyDefault{Namespace: "native:twitter", Definition: definition})
		return err
	}))
	for _, disabled := range []bool{false, true} {
		collection := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Twitter", Kind: "feed", Namespace: "native:twitter", State: "active"}})
		if disabled {
			optedOut := definition
			optedOut.Enabled = false
			putTranslationPolicy(t, repo, collection, 0, optedOut)
		}
		post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: collection.UUID}, "")
		text := "日本語"
		capture := translationPolicyCapture(t, repo, collection, post.UUID, nil, &text)
		decision := scheduleTranslationCapture(t, repo, capture, time.Now().UTC())
		if disabled {
			require.Equal(t, "disabled", decision.Status)
		} else {
			require.Equal(t, "recorded", decision.Status)
			require.Len(t, decision.Entries, 1)
			require.Equal(t, "caption", decision.Entries[0].Field)
		}
	}
}

func TestTranslationDisplayAppliesCachedTextDuringLaterMediaIntake(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	post := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "display-before-intake"}, "")
	text := "日本語の見出し\n元の説明"
	input := sourceTestCapture(t, post.UUID, 1, "Profile")
	input.Metadata.Title, input.Metadata.OriginalText = nil, &text
	capture := recordSourceTestCapture(t, f.repo, input)
	manifest := recordAttachmentManifest(t, f.repo, models.SourceAttachmentManifestInput{CaptureUUID: capture.UUID, Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "translated-attachment")}})
	attachment := manifestEntries(t, f.repo, manifest.UUID)[0].Attachment
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return f.repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, CaptureUUID: capture.UUID})
	}))
	require.NoError(t, applyMediaChoice(f.repo, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision,
		State: "linked", MediaUUID: f.entity.UUID, ExpectedMediaRevision: f.entity.Revision, Origin: "review"}))
	request := retainTranslationRequest(t, f.repo, text)
	retainTranslationCache(t, f.repo, models.TranslationCacheInput{RequestUUID: request.UUID, Status: "translated", TranslatedText: translationPointer("English heading\nEnglish description"), SourceLanguage: translationPointer("ja"), Provider: translationPointer("fixture"), CapturedAt: time.Now().UTC().Format(time.RFC3339Nano), Origin: "worker"})
	backfillDisplay(t, f.repo, post.UUID, false)
	policy := f.put(t, 0, models.MetadataPolicyRule{OnCreate: true, OnExisting: true, Mappings: map[string]models.MetadataMapping{
		"title":   {JQ: `.source.metadata.original_text | split("\n") | first`},
		"details": {JQ: `.source.metadata.original_text`},
	}})
	context := f.input(policy)
	context.Source = &metadata.Source{CaptureUUID: capture.UUID, AttachmentUUID: attachment.UUID}
	preview := policyPreview(t, f.repo, context)
	require.JSONEq(t, `"English heading"`, string(policyChange(t, preview, "title").Value))
	require.JSONEq(t, `"English heading\nEnglish description"`, string(policyChange(t, preview, "details").Value))
	// A deliberate title expression remains authoritative.
	policy = f.put(t, policy.Revision, models.MetadataPolicyRule{OnCreate: true, OnExisting: true, Mappings: map[string]models.MetadataMapping{
		"title": {JQ: `"Custom: " + .source.metadata.original_text`},
	}})
	context.PolicyRevision = policy.Revision
	preview = policyPreview(t, f.repo, context)
	require.JSONEq(t, `"Custom: 日本語の見出し\n元の説明"`, string(policyChange(t, preview, "title").Value))
}
