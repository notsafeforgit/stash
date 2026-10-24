package sqlite_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func policySource(t *testing.T, f metadataPolicyFixture, title, published string) *metadata.Source {
	t.Helper()
	post := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: uuid.NewString()}, "")
	input := sourceTestCapture(t, post.UUID, 1, "Biography")
	details := title + " description"
	input.Metadata.Title, input.Metadata.OriginalText, input.Metadata.PublishedAt = &title, &details, &published
	capture := recordSourceTestCapture(t, f.repo, input)
	manifest := recordAttachmentManifest(t, f.repo, models.SourceAttachmentManifestInput{CaptureUUID: capture.UUID, Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "shared-file")}})
	attachment := manifestEntries(t, f.repo, manifest.UUID)[0].Attachment
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return f.repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, CaptureUUID: capture.UUID})
	}))
	entity := metadataState(t, f.repo, f.entity.UUID, "title").Entity
	require.NoError(t, applyMediaChoice(f.repo, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision,
		State: "linked", MediaUUID: entity.UUID, ExpectedMediaRevision: entity.Revision, Origin: "review"}))
	_, err := observePostURL(f.repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(post.UUID), URL: "https://source.test/" + title})
	require.NoError(t, err)
	return &metadata.Source{CaptureUUID: capture.UUID, AttachmentUUID: attachment.UUID}
}

func TestMetadataPolicyRetainsFirstPostMetadataAndAddsURLs(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	policy := f.put(t, 0, models.MetadataPolicyRule{OnCreate: true, OnExisting: true, Mappings: map[string]models.MetadataMapping{
		"title": {JQ: `.source.metadata.title`}, "details": {JQ: `.source.metadata.original_text`},
		"date": {JQ: `.source.metadata.published_at[:10]`}, "urls": {JQ: `.source.urls`},
	}})
	first := f.input(policy)
	first.Source = policySource(t, f, "first", "2026-09-12T12:00:00Z")
	_, err := applyPolicy(f.repo, first, "")
	require.NoError(t, err)
	before := map[string][]models.MetadataFieldDecision{}
	for _, field := range []string{"title", "details", "date"} {
		before[field] = metadataHistory(t, f.repo, f.entity.UUID, field)
	}
	second := f.input(policy)
	second.Created = false
	// Published earlier, encountered later: source dates do not choose canonical metadata.
	second.Source = policySource(t, f, "second", "2025-01-01T00:00:00Z")
	preview := policyPreview(t, f.repo, second)
	require.Empty(t, preview.ReviewFields())
	for field := range before {
		require.Equal(t, "retained", policyChange(t, preview, field).Status)
	}
	require.JSONEq(t, `["https://source.test/first","https://source.test/second"]`, string(policyChange(t, preview, "urls").Value))
	applied, err := applyPolicy(f.repo, second, preview.Digest)
	require.NoError(t, err)
	require.Equal(t, []string{"urls"}, applied.AppliedFields())
	require.Empty(t, applied.ReviewFields())
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	for _, input := range []metadata.Input{first, second, first} {
		replayed, err := applyPolicy(f.repo, input, "")
		require.NoError(t, err)
		require.Empty(t, replayed.AppliedFields())
		require.Empty(t, replayed.ReviewFields())
	}
	for field, history := range before {
		require.Equal(t, history, metadataHistory(t, f.repo, f.entity.UUID, field), "retention must not replace the original decision")
	}
	require.JSONEq(t, `"first"`, string(metadataState(t, f.repo, f.entity.UUID, "title").Value))
	require.JSONEq(t, `"first description"`, string(metadataState(t, f.repo, f.entity.UUID, "details").Value))
	require.JSONEq(t, `"2026-09-12"`, string(metadataState(t, f.repo, f.entity.UUID, "date").Value))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		posts, err := f.repo.SourcePostMedia.PostsForMedia(ctx, f.entity.UUID, "", 100)
		require.NoError(t, err)
		require.Len(t, posts, 2)
		titles := []string{}
		for _, post := range posts {
			require.False(t, post.Association.Suppressed())
			require.Equal(t, 1, post.LinkedAttachments, "each post remains linked through its attachment")
			titles = append(titles, *post.LatestCapture.Title)
		}
		require.ElementsMatch(t, []string{"first description", "second description"}, titles, "Twitter headings use each post's own caption")
		return nil
	}))
}

func TestMetadataPolicySourceRetainsDefaultsWithinCollection(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	rule := models.MetadataPolicyRule{OnCreate: true, OnExisting: true, Mappings: map[string]models.MetadataMapping{
		"title": {JQ: `empty`, Fallback: json.RawMessage(`"First default"`)}, "code": {Value: json.RawMessage(`"First code"`)},
	}}
	policy := f.put(t, 0, rule)
	first := f.input(policy)
	first.Source = policySource(t, f, "first", "2026-09-12T12:00:00Z")
	_, err := applyPolicy(f.repo, first, "")
	require.NoError(t, err)
	rule.Mappings["title"] = models.MetadataMapping{JQ: `.source.metadata.title`}
	rule.Mappings["code"] = models.MetadataMapping{Value: json.RawMessage(`"Later code"`)}
	policy = f.put(t, policy.Revision, rule)
	second := f.input(policy)
	second.Source = policySource(t, f, "second", "2025-01-01T00:00:00Z")
	applied, err := applyPolicy(f.repo, second, "")
	require.NoError(t, err)
	require.Empty(t, applied.AppliedFields())
	require.Empty(t, applied.ReviewFields())
	require.Equal(t, "retained", policyChange(t, applied, "title").Status)
	require.Equal(t, "retained", policyChange(t, applied, "code").Status)
	require.JSONEq(t, `"First default"`, string(metadataState(t, f.repo, f.entity.UUID, "title").Value))
	require.JSONEq(t, `"First code"`, string(metadataState(t, f.repo, f.entity.UUID, "code").Value))
}

func TestMetadataPolicySourceURLUnionEnforcesLimitsWithoutDroppingLinks(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	rule := models.MetadataPolicyRule{OnCreate: true, OnExisting: true, Mappings: map[string]models.MetadataMapping{
		"urls": {JQ: `[range(0;4096) | "https://source.test/\(.)"]`},
	}}
	policy := f.put(t, 0, rule)
	first := f.input(policy)
	first.Source = policySource(t, f, "first", "2026-09-12T12:00:00Z")
	_, err := applyPolicy(f.repo, first, "")
	require.NoError(t, err)
	before := metadataState(t, f.repo, f.entity.UUID, "urls")
	second := f.input(policy)
	second.Source = policySource(t, f, "second", "2025-01-01T00:00:00Z")
	replay, err := applyPolicy(f.repo, second, "")
	require.NoError(t, err)
	require.Empty(t, replay.ReviewFields(), "duplicates at the limit do not overflow the union")
	require.Empty(t, replay.AppliedFields())
	rule.Mappings["urls"] = models.MetadataMapping{JQ: `["https://source.test/new"]`}
	policy = f.put(t, policy.Revision, rule)
	second.PolicyRevision = policy.Revision
	preview := policyPreview(t, f.repo, second)
	require.Equal(t, []string{"urls"}, preview.ReviewFields())
	require.Contains(t, policyChange(t, preview, "urls").Message, "4096")
	replay, err = applyPolicy(f.repo, second, preview.Digest)
	require.NoError(t, err)
	require.Empty(t, replay.AppliedFields())
	require.Equal(t, before.Value, metadataState(t, f.repo, f.entity.UUID, "urls").Value)
	require.Equal(t, before.Decision, metadataState(t, f.repo, f.entity.UUID, "urls").Decision)
}

func TestMetadataPolicyRetainsAcrossCollectionsWithoutEvaluatingUnusedAlternatives(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	performer := archiveFind(t, f.repo, models.ArchivePerformer, 71)
	policy := f.put(t, 0, models.MetadataPolicyRule{OnCreate: true, OnExisting: true, Mappings: map[string]models.MetadataMapping{
		"title": {JQ: `.source.metadata.title`}, "performers": {Value: json.RawMessage(`["` + performer.UUID + `"]`)},
	}})
	first := f.input(policy)
	first.Source = policySource(t, f, "first", "2026-09-12T12:00:00Z")
	_, err := applyPolicy(f.repo, first, "")
	require.NoError(t, err)
	other := f
	other.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
		Label: "Reposts", Kind: "directory", State: "active", RootUUID: &f.root.UUID, PathPrefix: ".",
	}})
	policy = other.put(t, 0, models.MetadataPolicyRule{OnCreate: true, OnExisting: true, MarkOrganized: true, OrganizedRequires: []string{"title", "performers", "details"}, Mappings: map[string]models.MetadataMapping{
		"title": {JQ: `error("unused alternative")`}, "performers": {JQ: `["Unknown performer"]`, ReferenceNames: true},
		"details": {JQ: `.source.metadata.original_text`}, "urls": {JQ: `.source.urls`},
	}})
	second := other.input(policy)
	second.Created = false
	second.Source = policySource(t, other, "second", "2025-01-01T00:00:00Z")
	preview := policyPreview(t, f.repo, second)
	require.Equal(t, "retained", policyChange(t, preview, "title").Status)
	require.Equal(t, "retained", policyChange(t, preview, "performers").Status)
	require.Empty(t, preview.ReviewFields(), "unused alternative expressions and names must not require review")
	applied, err := applyPolicy(f.repo, second, preview.Digest)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"details", "urls", "organized"}, applied.AppliedFields(), "a field never selected by the first post may be filled later")
	require.JSONEq(t, `"first"`, string(metadataState(t, f.repo, f.entity.UUID, "title").Value))
	require.JSONEq(t, `["`+performer.UUID+`"]`, string(metadataState(t, f.repo, f.entity.UUID, "performers").Value))
	// Explicit user choices, including clears, still take precedence over both posts.
	for _, field := range []string{"title", "urls"} {
		_, err := applyMetadata(f.repo, metadataInput(metadataState(t, f.repo, f.entity.UUID, field), "clear", "review", ""), false)
		require.NoError(t, err)
	}
	replayed, err := applyPolicy(f.repo, second, "")
	require.NoError(t, err)
	require.Equal(t, "protected", policyChange(t, replayed, "title").Status)
	require.Equal(t, "protected", policyChange(t, replayed, "urls").Status)
	require.JSONEq(t, `""`, string(metadataState(t, f.repo, f.entity.UUID, "title").Value))
	require.JSONEq(t, `[]`, string(metadataState(t, f.repo, f.entity.UUID, "urls").Value))
}
