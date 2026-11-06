package sqlite_test

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sourceadapter"
	"github.com/stretchr/testify/require"
)

func TestSavedTwitterDateUsesUnambiguousIdentityWithoutChangingCaptures(t *testing.T) {
	for _, scenario := range []string{"native", "url", "ambiguous_urls", "existing_date", "other_platform"} {
		t.Run(scenario, func(t *testing.T) {
			_, repo := archiveTestDatabase(t)
			ref := models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "saved-post"}
			if scenario == "native" {
				ref = models.SourcePostIdentifier{Namespace: "native:twitter", Value: "1555857150810865666"}
			}
			post := sourceTestPost(t, repo, ref, "")
			input := contentTestCapture(t, post.UUID, 0, "Saved caption")
			input.Origin, input.Platform = "legacy-nfo", "twitter"
			if scenario == "other_platform" {
				input.Platform = "reddit"
			}
			if scenario == "existing_date" {
				date, basis := "2022-08-07", "legacy-nfo-unverified"
				input.Metadata.PublishedAt, input.Metadata.DateBasis = &date, &basis
			}
			capture := recordSourceTestCapture(t, repo, input)
			_, err := observePostURL(repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(post.UUID), URL: "https://x.com/i/web/status/1555857150810865666"})
			require.NoError(t, err)
			if scenario == "ambiguous_urls" {
				_, err := observePostURL(repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(post.UUID), URL: "https://x.com/i/web/status/1552285545182531584"})
				require.NoError(t, err)
			}
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				content, err := repo.SourceEvidence.PostContent(ctx, post.UUID)
				require.NoError(t, err)
				require.Equal(t, "Saved caption", *content.OriginalText)
				switch scenario {
				case "ambiguous_urls", "other_platform":
					require.Nil(t, content.PublishedAt)
				case "existing_date":
					require.Equal(t, "2022-08-07", *content.PublishedAt)
					require.Equal(t, "legacy-nfo-unverified", *content.DateBasis)
				default:
					require.Equal(t, "2022-08-06T10:03:45.838Z", *content.PublishedAt)
					require.Equal(t, sourceadapter.TwitterDateBasis, *content.DateBasis)
				}
				stored, err := repo.SourceEvidence.FindCapture(ctx, capture.UUID)
				require.NoError(t, err)
				require.Equal(t, capture.Metadata, stored.Metadata)
				return nil
			}))
		})
	}
}
