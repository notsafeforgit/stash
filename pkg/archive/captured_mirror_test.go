package archive

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMirrorAttachmentMembershipSharesProducerContract(t *testing.T) {
	testCapturedMembershipContract(t, "testdata/captured-mirror-media-v1.json")
}

func testCapturedMembershipContract(t *testing.T, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	var fixtures struct {
		Cases []struct {
			Name   string
			Source json.RawMessage
			Error  bool
			Items  []*struct{ ID, Kind string }
		}
	}
	require.NoError(t, json.Unmarshal(body, &fixtures))
	for _, fixture := range fixtures.Cases {
		t.Run(fixture.Name, func(t *testing.T) {
			retained, err := RetainSourcePayload(fixture.Source)
			require.NoError(t, err)
			for _, source := range []json.RawMessage{fixture.Source, retained} {
				album, err := ExtractCapturedAlbum(source)
				if fixture.Error {
					require.Error(t, err)
					continue
				}
				require.NoError(t, err)
				if fixture.Items == nil {
					require.Nil(t, album)
					continue
				}
				post, err := ExtractCapturedPost(source)
				require.NoError(t, err)
				require.Equal(t, *post, album.Post)
				require.Equal(t, len(fixture.Items), *album.Manifest.ExpectedCount)
				require.Equal(t, len(fixture.Items) > 1, album.Manifest.DeclaredAlbum)
				complete, entries := true, 0
				for position, expected := range fixture.Items {
					if expected == nil {
						complete = false
						continue
					}
					actual := album.Manifest.Entries[entries]
					require.Equal(t, position, actual.Position)
					require.Equal(t, post.Namespace, actual.Reference.Namespace)
					require.Equal(t, expected.ID, actual.Reference.Value)
					require.Equal(t, expected.Kind, actual.MediaKind)
					entries++
				}
				require.Len(t, album.Manifest.Entries, entries)
				require.Equal(t, complete, album.Manifest.Complete)
			}
		})
	}
}

func TestMirrorNewEvidenceSharesPostBodyWithoutChangingLegacyCapturePartitions(t *testing.T) {
	raw := []byte(`{"category":"coomer","service":"onlyfans","user":"456","id":"123","title":"A post","file":{"path":"/image.jpg","id":"file1"},"attachments":[{"path":"/video.mp4","id":"file2"}],"mirror_media":{"version":1,"post":{"namespace":"mirror:coomer:onlyfans","value":"456/123"},"items":[{"id":"/image.jpg","kind":"image"},{"id":"/video.mp4","kind":"video"}]},"path":"/image.jpg","type":"file","url":"https://coomer.st/data/image.jpg","hash":"unverified-source-hash","num":1}`)
	first, err := PrepareRetainedCapture("gallery-dl", "coomer", raw)
	require.NoError(t, err)
	var value sourceObject
	require.NoError(t, json.Unmarshal(raw, &value))
	value["path"], value["type"], value["url"], value["hash"], value["num"] = "/video.mp4", "attachment", "https://coomer.st/data/video.mp4", "another-source-hash", 2
	body, err := json.Marshal(value)
	require.NoError(t, err)
	second, err := PrepareRetainedCapture("gallery-dl", "coomer", body)
	require.NoError(t, err)
	require.Equal(t, first.Shared, second.Shared)
	require.NotEqual(t, first.Patch, second.Patch)
	restored, err := RestoreCapture(second)
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(restored))
	delete(value, "mirror_media")
	body, err = json.Marshal(value)
	require.NoError(t, err)
	legacy, err := PrepareRetainedCapture("gallery-dl", "coomer", body)
	require.NoError(t, err)
	var shared sourceObject
	require.NoError(t, json.Unmarshal(legacy.Shared, &shared))
	require.Equal(t, "/video.mp4", shared["path"])
	require.Equal(t, "another-source-hash", shared["hash"])
}
