package archive

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSocialAttachmentMembershipSharesProducerContract(t *testing.T) {
	testCapturedMembershipContract(t, "testdata/captured-social-media-v1.json")
}

func TestSocialNewEvidenceSharesPostBodiesAndPreservesLegacyPartitions(t *testing.T) {
	body, err := os.ReadFile("testdata/captured-social-media-v1.json")
	require.NoError(t, err)
	var fixtures struct {
		Cases []struct{ Source json.RawMessage }
	}
	require.NoError(t, json.Unmarshal(body, &fixtures))
	for _, fixture := range fixtures.Cases {
		data, err := DecodeJSONObject(fixture.Source, MaxSourcePayloadBytes)
		require.NoError(t, err)
		album, err := ExtractCapturedAlbum(fixture.Source)
		if err != nil || album == nil {
			continue
		}
		category := data["category"].(string)
		first, err := PrepareRetainedCapture("gallery-dl", category, fixture.Source)
		require.NoError(t, err)
		field := "description"
		if category == "tiktok" {
			field = "title"
		}
		data[field], data["width"], data["height"], data["num"], data["filename"] = "Per-file value", 2048, 1024, 2, "another-file"
		body, err := EncodeSourceJSON(data)
		require.NoError(t, err)
		second, err := PrepareRetainedCapture("gallery-dl", category, body)
		require.NoError(t, err)
		require.Equal(t, first.Shared, second.Shared)
		require.NotEqual(t, first.Patch, second.Patch)
		restored, err := RestoreCapture(second)
		require.NoError(t, err)
		require.JSONEq(t, string(body), string(restored))
		delete(data, category+"_media")
		body, err = EncodeSourceJSON(data)
		require.NoError(t, err)
		legacy, err := PrepareRetainedCapture("gallery-dl", category, body)
		require.NoError(t, err)
		var shared sourceObject
		require.NoError(t, json.Unmarshal(legacy.Shared, &shared))
		require.Equal(t, "Per-file value", shared[field])
	}
}
