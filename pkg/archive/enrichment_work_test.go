package archive_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestEnrichmentDirectPostAdmission(t *testing.T) {
	for _, raw := range []string{
		"https://www.reddit.com/r/example/comments/abc123/title/?sort=new", "https://old.reddit.com/comments/abc123/",
		"https://reddit.com/user/example/comments/abc123/title/", "https://reddit.com/gallery/abc123", "https://redd.it/abc123",
		"https://x.com/example/status/123456/photo/2", "https://twitter.com/i/web/status/123456",
		"https://bsky.app/profile/did:plc:example/post/abc123", "https://bsky.app/profile/example.test/post/abc123",
		"https://www.tiktok.com/@example/video/123456", "https://tiktok.com/@example/photo/123456",
		"https://www.instagram.com/p/AbC_123-XYZ/", "https://instagram.com/reel/AbC_123/",
		"https://coomer.st/onlyfans/user/example/post/123", "https://coomer.cr/fansly/user/123/post/456",
		"https://kemono.cr/patreon/user/123/post/456", "https://kemono.su/fanbox/user/example/post/456",
		"https://www.patreon.com/posts/example-title-123", "https://patreon.com/posts/123", "https://fansly.com/post/123",
	} {
		require.True(t, archive.EnrichmentPostURL(raw), raw)
	}
	for _, raw := range []string{
		"", "https://reddit.com", "https://reddit.com/user/example/submitted/", "https://reddit.com/r/example/",
		"https://reddit.com/comments/abc123/title/def456", "https://i.redd.it/abc123.jpg", "https://preview.redd.it/abc123.jpg",
		"https://x.com/example", "https://x.com/example/media", "https://x.com/search?q=abc", "https://t.co/abc123",
		"https://bsky.app/profile/example.test", "https://tiktok.com/@example", "https://instagram.com/example/",
		"https://coomer.st/onlyfans/user/example", "https://kemono.cr/patreon/user/123", "https://kemono.cr/data/original.mp4",
		"https://patreon.com/example", "https://fansly.com/example", "https://onlyfans.com/123/example",
		"https://www.reddit.com.attacker.test/comments/abc123", "https://reddit.com@127.0.0.1/comments/abc123",
		"https://secret@reddit.com/comments/abc123", "https://reddit.com:8443/comments/abc123", "file:///comments/abc123",
		"https://reddit.com/comments/abc123\n", " https://reddit.com/comments/abc123", "https://reddit.com/comments/abc123/" + strings.Repeat("x", 8192),
	} {
		require.False(t, archive.EnrichmentPostURL(raw), raw)
	}
}

func TestEnrichmentIdentityAndCompletionBindExactInputs(t *testing.T) {
	input := models.EnrichmentTargetInput{PostUUID: uuid.NewString(), URLUUID: uuid.NewString(), CollectionUUID: uuid.NewString(), CollectionRevision: 1, Policy: models.EnrichmentGalleryMetadataV1, Origin: "migration"}
	id, err := archive.EnrichmentTargetIdentity(input)
	require.NoError(t, err)
	other := input
	other.Origin = "review"
	repeated, err := archive.EnrichmentTargetIdentity(other)
	require.NoError(t, err)
	require.Equal(t, id, repeated, "reviewing existing work must not create another target")
	other.CollectionRevision++
	changed, err := archive.EnrichmentTargetIdentity(other)
	require.NoError(t, err)
	require.NotEqual(t, id, changed, "a later source binding is a different target")
	for _, invalid := range []models.EnrichmentTargetInput{
		{}, {PostUUID: input.PostUUID, URLUUID: input.URLUUID, CollectionUUID: input.CollectionUUID, CollectionRevision: 0, Policy: input.Policy, Origin: input.Origin},
		{PostUUID: uuid.Nil.String(), URLUUID: input.URLUUID, CollectionUUID: input.CollectionUUID, CollectionRevision: 1, Policy: input.Policy, Origin: input.Origin},
		{PostUUID: input.PostUUID, URLUUID: input.URLUUID, CollectionUUID: input.CollectionUUID, CollectionRevision: 1, Policy: "unversioned", Origin: input.Origin},
	} {
		_, err := archive.EnrichmentTargetIdentity(invalid)
		require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
	}
	completion := models.EnrichmentCompletionInput{UUID: uuid.NewString(), TargetUUID: id, ExpectedRevision: 1, CaptureUUIDs: []string{uuid.NewString(), uuid.NewString()}}
	before := append([]string(nil), completion.CaptureUUIDs...)
	prepared, err := archive.PrepareEnrichmentCompletion(completion)
	require.NoError(t, err)
	require.Equal(t, before, completion.CaptureUUIDs, "normalization must not mutate caller data")
	require.Less(t, prepared.CaptureUUIDs[0], prepared.CaptureUUIDs[1])
	for _, captures := range [][]string{nil, {uuid.Nil.String()}, {"invalid"}, {before[0], before[0]}, make([]string, archive.MaxEnrichmentCaptures+1)} {
		completion.CaptureUUIDs = captures
		_, err := archive.PrepareEnrichmentCompletion(completion)
		require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
	}
}
