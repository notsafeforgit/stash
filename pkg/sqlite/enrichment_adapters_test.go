package sqlite_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestEnrichmentPublishesSupportedPostAdaptersAndChildren(t *testing.T) {
	for _, tc := range []struct {
		name, namespace, value, url, source, publisher string
	}{
		{"bluesky", "native:bluesky", "did:plc:example/3abc", "https://bsky.app/profile/did:plc:example/post/3abc", `{"category":"bluesky","uri":"at://did:plc:example/app.bsky.feed.post/3abc","author":{"did":"did:plc:example","handle":"example.test"},"text":"Original caption","createdAt":"2026-10-01T12:00:00Z"}`, "did:plc:example"},
		{"tiktok", "native:tiktok", "9007199254740993", "https://www.tiktok.com/@example/video/9007199254740993", `{"category":"tiktok","id":9007199254740993,"author":{"id":"456","uniqueId":"example"},"desc":"Original caption","date":"2026-10-01T12:00:00Z"}`, "456"},
		{"instagram", "native:instagram", "123", "https://www.instagram.com/p/Example/", `{"category":"instagram","post_id":123,"sidecar_media_id":123,"owner_id":"456","username":"example","description":"Original caption","post_date":"2026-10-01T12:00:00Z","date":"2020-01-01T00:00:00Z"}`, "456"},
		{"patreon", "native:patreon", "123", "https://www.patreon.com/posts/123", `{"category":"patreon","id":123,"creator":{"id":"456"},"content":"Original caption","published_at":"2026-10-01T12:00:00Z"}`, "456"},
		{"fansly", "native:fansly", "123", "https://fansly.com/post/123", `{"category":"fansly","id":"123","account":{"id":"456","username":"example"},"content":"Original caption","date":"2026-10-01T12:00:00Z"}`, "456"},
		{"coomer", "mirror:coomer:onlyfans", "456/123", "https://coomer.st/onlyfans/user/456/post/123", `{"category":"coomer","service":"onlyfans","user":"456","id":"123","content":"Original caption","published":"2026-10-01T12:00:00Z"}`, "456"},
		{"kemono", "mirror:kemono:patreon", "456/123", "https://kemono.cr/patreon/user/456/post/123", `{"category":"kemono","service":"patreon","user":"456","id":"123","content":"Original caption","published":"2026-10-01T12:00:00Z"}`, "456"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref := models.SourcePostIdentifier{Namespace: tc.namespace, Value: tc.value}
			f := newEnrichmentExecutionFixtureForPost(t, ref, tc.url, models.SourceCollectionDefinition{
				Label: "Fixture source", Kind: "feed", Namespace: tc.namespace, State: "active", TargetURL: tc.url})
			root, err := archive.DecodeJSONObject([]byte(tc.source), archive.MaxSourcePayloadBytes)
			require.NoError(t, err)
			root["source_extractor_url"] = tc.url
			root["_parent"] = map[string]any{"category": tc.name, "subcategory": "user", "title": "Profile title", "author": map[string]any{"id": "feed-owner"}}
			parent := 0
			observed := "2026-10-03T01:00:00.123456Z"
			body, err := json.Marshal(archive.EnrichmentTranscript{
				Schema: archive.EnrichmentTranscriptSchema, URL: tc.url, RetentionPolicy: archive.SourceRetentionVersion, ExtractorVersion: "1.32.15-dev",
				Records: []archive.EnrichmentRecord{
					{Kind: "post", Patch: root, Removed: []string{}, ObservedAt: observed},
					{Kind: "post", Parent: &parent, Patch: map[string]any{"category": "imgur", "id": "child", "source_extractor_url": "https://imgur.com/child", "title": "Child title", "date": "2020-01-01T00:00:00Z", "user": map[string]any{"id": "other"}}, Removed: []string{}, ObservedAt: observed},
				}, Pending: []archive.EnrichmentReference{}, Unresolved: []archive.EnrichmentReference{},
			})
			require.NoError(t, err)
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, body)
			require.NoError(t, err)
			publication, err := f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
			require.NoError(t, err)
			require.Equal(t, 2, publication.CaptureCount)
			require.Zero(t, publication.UnresolvedCount)
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				records, err := f.repo.EnrichmentJob.PublishedRecords(ctx, job.UUID, -1, 10)
				require.NoError(t, err)
				require.Len(t, records, 2)
				for _, record := range records {
					capture, err := f.repo.SourceEvidence.FindCapture(ctx, record.CaptureUUID)
					require.NoError(t, err)
					require.Equal(t, f.target.PostUUID, capture.PostUUID)
					require.Equal(t, tc.name, capture.Platform)
					require.Equal(t, "Original caption", *capture.Metadata.OriginalText)
					require.Equal(t, "2026-10-01T12:00:00Z", *capture.Metadata.PublishedAt)
					require.Equal(t, "source", *capture.Metadata.DateBasis)
					raw, err := archive.RestoreCapture(capture.Payload)
					require.NoError(t, err)
					post, err := archive.ExtractCapturedPost(raw)
					require.NoError(t, err)
					require.Equal(t, ref, *post)
					publisher, err := f.repo.CapturePublisher.Current(ctx, capture.UUID)
					require.NoError(t, err)
					require.NotNil(t, publisher.AccountUUID)
					account, err := archive.ExtractCapturedAccount(raw)
					require.NoError(t, err)
					require.Equal(t, tc.namespace, account.Namespace)
					require.Equal(t, tc.publisher, account.Identifiers[0].Reference.Value)
					selected, err := f.repo.SourceAttachment.Selection(ctx, capture.PostUUID)
					require.NoError(t, err)
					require.Nil(t, selected, "post identity alone does not prove an attachment list")
				}
				return nil
			}))
			// Audit the new namespace, retained bytes and publisher proof, then
			// verify lost-response recovery after restart and staging release.
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.AuditForTesting(f.db.DatabasePath()))
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
			f.repo = f.db.Repository()
			f.worker.Service = ingest.New(f.repo)
			f.now = f.now.Add(time.Hour)
			replay, err := f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
			require.NoError(t, err)
			require.Equal(t, publication, replay)
		})
	}
}
