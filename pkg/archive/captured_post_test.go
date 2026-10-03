package archive

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCapturedPostAdaptersShareProducerContract(t *testing.T) {
	body, err := os.ReadFile("testdata/captured-post-adapters-v1.json")
	require.NoError(t, err)
	var fixture struct {
		Cases []struct {
			Name     string
			Source   json.RawMessage
			Post     *models.SourcePostIdentifier
			Platform string
			Metadata models.SourcePostMetadata
		}
	}
	require.NoError(t, json.Unmarshal(body, &fixture))
	for _, c := range fixture.Cases {
		t.Run(c.Name, func(t *testing.T) {
			post, err := ExtractCapturedPost(c.Source)
			if c.Post == nil {
				require.True(t, err != nil || post == nil, "insufficient/contradictory evidence must not create a source identity")
				return
			}
			require.NoError(t, err)
			require.Equal(t, c.Post, post)
			require.Equal(t, c.Platform, CapturedPostPlatform(*post))
			metadata, err := CapturedMetadata(c.Source)
			require.NoError(t, err)
			require.Equal(t, c.Metadata, metadata)
			kept, err := RetainSourcePayload(c.Source)
			require.NoError(t, err)
			again, err := ExtractCapturedPost(kept)
			require.NoError(t, err)
			require.Equal(t, post, again)
		})
	}
}

func TestCapturedPostAncestryAlsoScopesPublishersAndAlbums(t *testing.T) {
	raw := []byte(`{"category":"imgur","id":"child","_parent":{"category":"redgifs","id":"host","_reddit":{
 "id":"root","author_fullname":"t2_publisher","author":"publisher","gallery_data":{"items":[{"media_id":"photo"}]}}}}`)
	account, err := ExtractCapturedAccount(raw)
	require.NoError(t, err)
	require.Equal(t, "native:reddit", account.Namespace)
	require.Equal(t, "/_parent/_reddit/author_fullname", account.Identifiers[0].Path)
	album, err := ExtractCapturedAlbum(raw)
	require.NoError(t, err)
	require.Equal(t, "root", album.Post.Value)
	require.Equal(t, "/_parent/_reddit/gallery_data/items", album.EvidencePath)
	account, err = ExtractCapturedAccount([]byte(`{"category":"imgur","id":"child","_parent":{"category":"fansly","id":"post","account":{"id":"123","username":"publisher"}}}`))
	require.NoError(t, err)
	require.Equal(t, "native:fansly", account.Namespace)
	require.Equal(t, "123", account.Identifiers[0].Reference.Value)
	require.Equal(t, "/_parent/account/id", account.Identifiers[0].Path)
}
