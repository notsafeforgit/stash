package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCanonicalPostBrowserPreservesOriginalEvidenceAcrossChainedMerges(t *testing.T) {
	db, repo := postIdentityFixture(t)
	const a = "00000000-0000-4000-8000-000000000001"
	const other = "00000000-0000-4000-8000-000000000002"
	const b = "00000000-0000-4000-8000-000000000003"
	const c = "00000000-0000-4000-8000-000000000005"
	const shared = "https://example.test/shared-post"
	clock := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	inputs := map[string]models.SourceCaptureInput{}
	var publisher *models.SourceAccount
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		for _, id := range []string{a, other, b, c} {
			namespace := "legacy:catalog:fixture"
			if id == other || id == c {
				namespace = "native:reddit"
			}
			_, err := repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: namespace, Value: id}, id)
			if err != nil {
				return err
			}
			for _, url := range []string{shared, "https://example.test/" + id} {
				_, err = repo.SourcePostLinks.ObserveURL(ctx, models.SourcePostURLInput{SourcePostEvidence: models.SourcePostEvidence{
					UUID: uuid.NewString(), PostUUID: id, Origin: "migration", Basis: "retained", ObservedAt: clock}, URL: url})
				if err != nil {
					return err
				}
			}
		}
		publisher, err = repo.SourceAccount.Create(ctx, "native:reddit", "Selected publisher")
		if err != nil {
			return err
		}
		for i, id := range []string{a, c, b} {
			payload, err := archive.PrepareRetainedCapture("gallery-dl", "reddit", []byte(`{"category":"reddit","id":"one-post"}`))
			if err != nil {
				return err
			}
			title := "Original capture " + id
			input := models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: id, Origin: "gallery-dl", Platform: "reddit",
				CapturedAt: clock.Add(time.Duration(i) * time.Hour), RetentionPolicy: archive.SourceRetentionVersion,
				Metadata: models.SourcePostMetadata{Title: &title}, Payload: *payload}
			if id == b {
				recorded := input.CapturedAt
				input.CapturedAt, input.RecordedAt = time.Time{}, &recorded
			}
			inputs[id] = input
			if _, err := repo.SourceEvidence.RecordCapture(ctx, input); err != nil {
				return err
			}
			if _, err := repo.SourceAttachment.RecordManifest(ctx, models.SourceAttachmentManifestInput{CaptureUUID: input.UUID,
				Entries: []models.SourceAttachmentEntry{{Position: 0, MediaKind: "image", Reference: models.SourcePostIdentifier{Namespace: "native:reddit", Value: "first"}}}}); err != nil {
				return err
			}
			if id == c {
				continue
			}
			preview, err := repo.CapturePublisher.Preview(ctx, input.UUID, publisher.UUID)
			if err != nil {
				return err
			}
			if _, err := repo.CapturePublisher.Apply(ctx, models.CapturePublisherInput{UUID: uuid.NewString(), CaptureUUID: input.UUID,
				ExpectedSignature: preview.Signature, Action: "link", AccountUUID: publisher.UUID, Origin: "review"}); err != nil {
				return err
			}
		}
		return nil
	}))
	media := consolidationTestMedia(t, repo)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		for _, post := range []string{a, other} {
			if _, err := repo.SourceAttachment.RecordMediaEvidence(ctx, models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: post,
				MediaUUID: media.UUID, Basis: "legacy", Details: []byte(`{}`)}); err != nil {
				return err
			}
		}
		return nil
	}))
	originalAttachmentChoice(t, repo, attachmentConsolidationInput(t, repo, a, "linked", media.UUID))
	originalAttachmentChoice(t, repo, attachmentConsolidationInput(t, repo, b, "linked", media.UUID))
	request := consolidationMediaRequest(t, repo, a, media.UUID, "linked", false)
	originalChoice := applyConsolidationMedia(t, repo, request, "")
	applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, b, media.UUID, "unlinked", false), "")
	tables := []string{"source_captures", "source_post_revisions", "source_post_urls", "source_post_url_evidence", "source_media_evidence", "source_post_identifiers"}
	before := identityRows(t, repo, tables...)
	publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, b, c))

	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, url := range []string{"", shared} {
			var found []string
			for after := ""; ; {
				page, err := repo.SourceEvidence.BrowsePosts(ctx, models.SourcePostFilter{URL: url, After: after, Limit: 1})
				require.NoError(t, err)
				if len(page) == 0 {
					break
				}
				found = append(found, page[0].UUID)
				require.Equal(t, page[0].UUID, page[0].RequestedUUID)
				after = page[0].UUID
			}
			require.Equal(t, []string{other, c}, found, "only reviewed identity merges collapse posts sharing a URL")
		}
		for _, id := range []string{a, b, c} {
			summary, err := repo.SourceEvidence.PostSummary(ctx, id)
			require.NoError(t, err)
			require.Equal(t, id, summary.RequestedUUID)
			require.Equal(t, c, summary.UUID)
			require.Equal(t, b, summary.LatestCapture.PostUUID)
			require.Equal(t, inputs[b].UUID, summary.LatestCapture.UUID)
			require.Nil(t, summary.LatestCapture.CapturedAt)
			require.NotNil(t, summary.LatestCapture.RecordedAt)
			require.True(t, summary.MoreURLs)
			page, err := repo.SourceEvidence.BrowsePosts(ctx, models.SourcePostFilter{PostUUID: id, After: other, Limit: 1})
			require.NoError(t, err)
			require.Equal(t, []models.SourcePostSummary{*summary}, page)
			key := models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: id}
			if id == c {
				key.Namespace = "native:reddit"
			}
			page, err = repo.SourceEvidence.BrowsePosts(ctx, models.SourcePostFilter{Identifier: &key, After: other, Limit: 1})
			require.NoError(t, err)
			require.Equal(t, c, page[0].UUID, "an early alias cannot be discarded before the canonical cursor")
			original, err := repo.SourceEvidence.FindPostByIdentifier(ctx, key)
			require.NoError(t, err)
			require.Equal(t, id, original.UUID, "ingestion lookup retains its original owner")
			publishers, err := repo.CapturePublisher.PostAccounts(ctx, id, "", 25)
			require.NoError(t, err)
			require.Len(t, publishers, 1)
			require.Equal(t, publisher.UUID, publishers[0].UUID)
			review, err := repo.SourcePostMedia.Review(ctx, id, media.UUID)
			require.NoError(t, err)
			require.Equal(t, id, review.RequestedPostUUID)
			require.Equal(t, c, review.Association.PostUUID)
			require.Equal(t, "conflict", review.Association.State)
			require.Len(t, review.Association.Decisions, 2)
			require.True(t, review.HasRetainedEvidence)
			require.Zero(t, review.LinkedAttachments, "unresolved attachment heads cannot claim a shared link")
			items, err := repo.SourcePostMedia.MediaForPost(ctx, id, "", 25)
			require.NoError(t, err)
			require.Len(t, items, 1)
			require.Equal(t, id, items[0].RequestedPostUUID)
			require.Equal(t, review.Association, items[0].Association)
		}
		posts, err := repo.SourcePostMedia.PostsForMedia(ctx, media.UUID, other, 25)
		require.NoError(t, err)
		require.Len(t, posts, 1)
		require.Equal(t, c, posts[0].Association.PostUUID)
		var urls []models.SourcePostURL
		for after := ""; ; {
			page, err := repo.SourcePostLinks.CurrentURLs(ctx, a, after, 1)
			require.NoError(t, err)
			if len(page) == 0 {
				break
			}
			urls = append(urls, page...)
			after = page[0].UUID
		}
		require.Len(t, urls, 4, "one stable original URL witness per exact value across all pages")
		values := make(map[string]bool)
		for _, url := range urls {
			require.True(t, slices.Contains([]string{a, b, c}, url.PostUUID))
			require.False(t, values[url.URL])
			values[url.URL] = true
		}
		var identifiers []models.SourcePostIdentifier
		for after := (*models.SourcePostIdentifier)(nil); ; {
			page, err := repo.SourceEvidence.CurrentPostIdentifiers(ctx, a, after, 1)
			require.NoError(t, err)
			if len(page) == 0 {
				break
			}
			identifiers = append(identifiers, page...)
			after = &page[0]
		}
		require.Len(t, identifiers, 3)
		var owners []string
		for after := (*models.SourceCaptureCursor)(nil); ; {
			page, err := repo.SourceEvidence.CurrentCaptures(ctx, a, after, 1)
			require.NoError(t, err)
			if len(page) == 0 {
				break
			}
			owners = append(owners, page[0].PostUUID)
			after = &models.SourceCaptureCursor{UUID: page[0].UUID, CapturedAt: page[0].CapturedAt, RecordedAt: page[0].RecordedAt}
		}
		require.Equal(t, []string{a, c, b}, owners)
		original, err := repo.SourceEvidence.Captures(ctx, a, nil, 25)
		require.NoError(t, err)
		require.Len(t, original, 1)
		require.Equal(t, inputs[a].UUID, original[0].UUID)
		return nil
	}))
	selected := applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, c, media.UUID, "linked", true), merge.UUID)
	attachmentSnapshot := attachmentConsolidationSnapshot(t, repo, c)
	attachmentInput := attachmentConsolidationInput(t, repo, c, "linked", media.UUID)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := publishConsolidatedAttachmentMedia(ctx, c, merge.UUID, attachmentSnapshot.Signature, attachmentInput)
		return err
	}))
	checkCanonicalPostURLPolicy(t, repo, inputs[a], selected, []string{
		shared, "https://example.test/" + a, "https://example.test/" + b, "https://example.test/" + c,
	})
	choice := applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, c, media.UUID, "unlinked", false), "")
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	require.Equal(t, before, identityRows(t, repo, tables...))
	require.Equal(t, originalChoice, applyConsolidationMedia(t, repo, request, ""), "saved original decision remains replayable")
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		review, err := repo.SourcePostMedia.Review(ctx, a, media.UUID)
		require.NoError(t, err)
		require.Equal(t, "unlinked", review.Association.State)
		require.Equal(t, []models.SourcePostMediaDecision{*choice}, review.Association.Decisions)
		require.Equal(t, 1, review.LinkedAttachments, "one shared attachment remains separate from whole-post rejection")
		for _, tc := range []struct {
			query string
			args  []interface{}
			index string
		}{
			{canonicalPostIdentifiersQuery, []interface{}{"", "", 2, a, 2}, "source_post_identifiers_post"},
			{canonicalPostURLsQuery, []interface{}{"", 2, a, 2}, "source_post_urls_page"},
			{canonicalPostCapturesQuery, []interface{}{"", "", 2, a, 2}, "source_captures_order"},
			{canonicalPostLatestCaptureQuery, []interface{}{a}, "source_captures_order"},
		} {
			var plan []struct {
				ID, Parent, Notused int
				Detail              string
			}
			require.NoError(t, dbWrapper.Select(ctx, &plan, "EXPLAIN QUERY PLAN "+tc.query, tc.args...))
			text := fmt.Sprint(plan)
			require.Contains(t, text, "source_post_identities_canonical")
			require.Contains(t, text, tc.index)
			require.NotContains(t, text, "source_payloads")
		}
		return nil
	}))
}

func checkCanonicalPostURLPolicy(t *testing.T, repo models.Repository, capture models.SourceCaptureInput, choice *models.SourcePostMediaDecision, want []string) {
	t.Helper()
	binding, err := archive.ProbeMediaRoot(t.TempDir())
	require.NoError(t, err)
	input := metadata.Input{EntityUUID: choice.MediaUUID, RelativePath: "original.mp4",
		Source: &metadata.Source{CaptureUUID: capture.UUID, PostMediaDecisionUUID: choice.UUID}}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		root, err := repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review",
			MediaRootDefinition: models.MediaRootDefinition{Label: "Retained media", State: "active", Binding: binding}})
		if err != nil {
			return err
		}
		collection, err := repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review",
			SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Imported source", Kind: "directory", State: "active", RootUUID: &root.UUID, PathPrefix: "."}})
		if err != nil {
			return err
		}
		if err := repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, CaptureUUID: capture.UUID}); err != nil {
			return err
		}
		policy, err := repo.MetadataPolicy.Put(ctx, models.MetadataPolicyInput{CollectionUUID: collection.UUID, ExpectedCollectionRevision: collection.Revision, Origin: "review",
			Definition: models.MetadataPolicyDefinition{Enabled: true, Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{
				models.ArchiveScene: {OnExisting: true, Mappings: map[string]models.MetadataMapping{
					"urls": {JQ: `.source | select(.urls_complete) | .urls | select(length > 0)`},
				}},
			}}})
		if err == nil {
			input.CollectionUUID, input.CollectionRevision, input.PolicyRevision = collection.UUID, collection.Revision, policy.Revision
		}
		return err
	}))
	slices.Sort(want)
	wantJSON, err := json.Marshal(want)
	require.NoError(t, err)
	service := metadata.Service{Repo: repo}
	var preview *metadata.Preview
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		preview, err = service.Preview(ctx, input)
		require.NoError(t, err)
		require.Equal(t, "ready", preview.State)
		require.Len(t, preview.Changes, 1)
		require.JSONEq(t, string(wantJSON), string(preview.Changes[0].Value))
		require.Equal(t, capture.UUID, preview.Changes[0].CaptureUUID)
		source := preview.Data["source"].(map[string]interface{})
		require.Equal(t, capture.PostUUID, source["post_uuid"], "current URLs do not rewrite the selected capture's provenance")
		field, err := repo.MetadataField.State(ctx, choice.MediaUUID, "urls")
		require.NoError(t, err)
		require.JSONEq(t, `[]`, string(field.Value), "preview does not mutate the entity")
		return nil
	}))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := service.Apply(ctx, input, preview.Digest)
		return err
	}))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		field, err := repo.MetadataField.State(ctx, choice.MediaUUID, "urls")
		require.NoError(t, err)
		require.JSONEq(t, string(wantJSON), string(field.Value))
		require.Equal(t, capture.UUID, *field.Decision.CaptureUUID)
		return nil
	}))
}
