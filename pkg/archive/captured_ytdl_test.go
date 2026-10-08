package archive

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCapturedYTDLSharedContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/captured-ytdl-v1.json")
	require.NoError(t, err)
	var fixture struct {
		Cases []struct {
			Name          string
			Source        json.RawMessage
			Post          *models.SourcePostIdentifier
			Attachment    *models.SourcePostIdentifier
			Metadata      models.SourcePostMetadata
			MetadataError bool `json:"metadata_error"`
		}
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))
	for _, c := range fixture.Cases {
		t.Run(c.Name, func(t *testing.T) {
			kept, err := RetainSourcePayload(c.Source)
			require.NoError(t, err)
			for _, body := range [][]byte{c.Source, kept} {
				post, err := ExtractCapturedPost(body)
				if c.Post == nil {
					require.True(t, err != nil || post == nil)
					continue
				}
				require.NoError(t, err)
				require.Equal(t, c.Post, post)
				metadata, err := CapturedMetadata(body)
				if c.MetadataError {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					require.Equal(t, c.Metadata, metadata)
				}
				album, err := ExtractCapturedAlbum(body)
				if c.Attachment == nil {
					require.True(t, err != nil || album == nil)
					continue
				}
				require.NoError(t, err)
				require.NotNil(t, album)
				require.Equal(t, *c.Post, album.Post)
				require.Len(t, album.Manifest.Entries, 1)
				require.Equal(t, *c.Attachment, album.Manifest.Entries[0].Reference)
				require.True(t, album.Manifest.Complete)
				require.False(t, album.Manifest.DeclaredAlbum, "playlist membership does not declare a source album")
				require.Equal(t, 1, *album.Manifest.ExpectedCount)
			}
		})
	}
}
