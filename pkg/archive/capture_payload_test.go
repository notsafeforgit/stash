package archive

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func canonicalSourceFixture(t *testing.T, raw []byte) json.RawMessage {
	t.Helper()
	value, err := DecodeJSONObject(raw, MaxSourcePayloadBytes)
	require.NoError(t, err)
	result, err := EncodeSourceJSON(value)
	require.NoError(t, err)
	return result
}

func TestNativeSourceRetentionReferenceFixtures(t *testing.T) {
	body, err := os.ReadFile("testdata/source-retention-v1.json")
	require.NoError(t, err)
	var fixtures struct {
		Policy string `json:"policy"`
		Cases  []struct {
			Name        string                          `json:"name"`
			Origin      string                          `json:"origin"`
			Platform    string                          `json:"platform"`
			Input       json.RawMessage                 `json:"input"`
			Retained    json.RawMessage                 `json:"retained"`
			Shared      json.RawMessage                 `json:"shared"`
			Patch       json.RawMessage                 `json:"patch"`
			Profiles    []models.SourceProfileBody      `json:"profiles"`
			ProfileRefs []models.SourceProfileReference `json:"profile_refs"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(body, &fixtures))
	require.Equal(t, SourceRetentionVersion, fixtures.Policy)
	for _, tc := range fixtures.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			original := bytes.Clone(tc.Input)
			retained, err := RetainSourcePayload(tc.Input)
			require.NoError(t, err)
			require.Equal(t, canonicalSourceFixture(t, tc.Retained), retained)
			repeated, err := RetainSourcePayload(retained)
			require.NoError(t, err)
			require.Equal(t, retained, repeated, "retention must be idempotent")
			capture, err := PrepareRetainedCapture(tc.Origin, tc.Platform, retained)
			require.NoError(t, err)
			require.Equal(t, canonicalSourceFixture(t, tc.Shared), capture.Shared)
			require.Equal(t, canonicalSourceFixture(t, tc.Patch), capture.Patch)
			expectedProfiles := make(map[string]models.SourceProfileBody)
			for _, profile := range tc.Profiles {
				profile.Body = canonicalSourceFixture(t, profile.Body)
				expectedProfiles[profile.Hash] = profile
			}
			actualProfiles := make(map[string]models.SourceProfileBody)
			for _, profile := range capture.Profiles {
				actualProfiles[profile.Hash] = profile
			}
			require.Equal(t, expectedProfiles, actualProfiles)
			require.Equal(t, tc.ProfileRefs, capture.Refs)
			reconstructed, err := RestoreCapture(capture)
			require.NoError(t, err)
			require.Equal(t, retained, reconstructed)
			require.Equal(t, original, []byte(tc.Input), "normalization must leave source input untouched")
		})
	}
}

func prepareSourceFixture(t *testing.T, input string) *models.SourceCapturePayload {
	t.Helper()
	retained, err := RetainSourcePayload([]byte(input))
	require.NoError(t, err)
	payload, err := PrepareRetainedCapture("gallery-dl", "twitter", retained)
	require.NoError(t, err)
	return payload
}

func TestNativeSourceRetentionDoesNotTreatMissingMediaIDsAsEqual(t *testing.T) {
	raw := []byte(`{"category":"reddit","url":"https://i.redd.it/","media_metadata":{"":{"s":{"u":"https://example.test/other.jpg"}}},"preview":{"images":[{"source":{"url":"https://example.test/keep.jpg","width":100,"height":100}}]}}`)
	retained, err := RetainSourcePayload(raw)
	require.NoError(t, err)
	require.Contains(t, string(retained), "https://example.test/keep.jpg", "an empty inferred media ID does not establish duplicate content")
}

func TestPostBodiesDoNotRepeatProfilesOrAttachmentMetadata(t *testing.T) {
	one := prepareSourceFixture(t, `{"category":"twitter","tweet_id":"one","content":"Caption","author":{"id":98765432109876543210,"name":"Example","description":"Bio","followers_count":1},"num":1,"filename":"first","_url":"https://example.test/one.mp4","source_user":{"id":"original","name":"Original"}}`)
	two := prepareSourceFixture(t, `{"category":"twitter","tweet_id":"one","content":"Caption","author":{"id":98765432109876543210,"name":"Example","description":"Bio","followers_count":999},"num":2,"filename":"second","_url":"https://example.test/two.mp4","source_user":{"id":"original","name":"Original"}}`)
	require.Equal(t, one.Shared, two.Shared)
	require.Equal(t, one.Profiles, two.Profiles)
	require.Equal(t, one.Refs, two.Refs)
	require.NotEqual(t, one.Patch, two.Patch)
	require.NotContains(t, string(one.Shared), "Bio")
	require.NotContains(t, string(one.Patch), "Original")
	reconstructed, err := RestoreCapture(one)
	require.NoError(t, err)
	require.Contains(t, string(reconstructed), "98765432109876543210")
	changed := prepareSourceFixture(t, `{"category":"twitter","tweet_id":"one","content":"Caption","author":{"id":98765432109876543210,"name":"Example","description":"Changed biography"},"num":1}`)
	require.Equal(t, one.Shared, changed.Shared, "a profile edit should not duplicate the post body")
	require.NotEqual(t, one.Profiles, changed.Profiles, "a meaningful profile edit retains a distinct profile body")
	another := prepareSourceFixture(t, `{"category":"twitter","tweet_id":"another","content":"Another caption","author":{"id":98765432109876543210,"name":"Example","description":"Changed biography"},"num":1}`)
	require.NotEqual(t, changed.Shared, another.Shared)
	require.Equal(t, changed.Profiles, another.Profiles, "one profile body is reusable across posts")
}

func TestRetainedCaptureBoundaryPreservesHistoricalEvidence(t *testing.T) {
	raw := []byte(`{"category":"twitter","author":{"name":"Historic","unknown_retained_field":"keep","followers_count":42},"settings":{"historical_source_value":true},"_runtime_field":"retained by older source","filename":"one"}`)
	payload, err := PrepareRetainedCapture("gallery-dl", "twitter", raw)
	require.NoError(t, err)
	restored, err := RestoreCapture(payload)
	require.NoError(t, err)
	require.Equal(t, canonicalSourceFixture(t, raw), restored, "partitioning for import must not silently apply a new retention policy")
	filtered, err := RetainSourcePayload(raw)
	require.NoError(t, err)
	require.NotContains(t, string(filtered), "followers_count")
	require.NotContains(t, string(filtered), "_runtime_field")
}

func TestCaptureReferencesCannotOverwriteOrInventSourceData(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*models.SourceCapturePayload)
	}{
		{"missing profile", func(p *models.SourceCapturePayload) { p.Profiles = nil }},
		{"unreferenced profile", func(p *models.SourceCapturePayload) { p.Refs = nil }},
		{"corrupt profile", func(p *models.SourceCapturePayload) { p.Profiles[0].Body = json.RawMessage(`{"name":"Different"}`) }},
		{"wrong namespace", func(p *models.SourceCapturePayload) { p.Profiles[0].Namespace = "native:reddit" }},
		{"duplicate reference", func(p *models.SourceCapturePayload) { p.Refs = append(p.Refs, p.Refs[0]) }},
		{"duplicate body", func(p *models.SourceCapturePayload) { p.Profiles = append(p.Profiles, p.Profiles[0]) }},
		{"non-null target", func(p *models.SourceCapturePayload) { p.Shared = json.RawMessage(`{"author":{"name":"Existing"}}`) }},
		{"missing target", func(p *models.SourceCapturePayload) { p.Refs[0].Path = "/absent" }},
		{"invalid escape", func(p *models.SourceCapturePayload) { p.Refs[0].Path = "/author~2" }},
		{"negative array index", func(p *models.SourceCapturePayload) {
			p.Shared = json.RawMessage(`{"items":[{"author":null}]}`)
			p.Refs[0].Path = "/items/-1/author"
		}},
		{"noncanonical array index", func(p *models.SourceCapturePayload) {
			p.Shared = json.RawMessage(`{"items":[{"author":null}]}`)
			p.Refs[0].Path = "/items/00/author"
		}},
		{"array bounds", func(p *models.SourceCapturePayload) {
			p.Shared = json.RawMessage(`{"items":[{"author":null}]}`)
			p.Refs[0].Path = "/items/9/author"
		}},
		{"wrong part", func(p *models.SourceCapturePayload) { p.Refs[0].Part = "untrusted" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := prepareSourceFixture(t, `{"category":"twitter","author":{"name":"Example"},"num":1}`)
			tc.edit(payload)
			_, err := RestoreCapture(payload)
			require.Error(t, err)
		})
	}
	outerBody, innerBody := json.RawMessage(`{"nested":null}`), json.RawMessage(`{"name":"Inner"}`)
	outer := models.SourceProfileBody{Namespace: "native:twitter", Body: outerBody, Hash: sourceProfileHash("native:twitter", outerBody)}
	inner := models.SourceProfileBody{Namespace: "native:twitter", Body: innerBody, Hash: sourceProfileHash("native:twitter", innerBody)}
	_, err := RestoreCapture(&models.SourceCapturePayload{Shared: json.RawMessage(`{"author":null}`), Patch: json.RawMessage(`{}`),
		Profiles: []models.SourceProfileBody{outer, inner}, Refs: []models.SourceProfileReference{
			{Part: "shared", Path: "/author", Hash: outer.Hash}, {Part: "shared", Path: "/author/nested", Hash: inner.Hash},
		}})
	require.Error(t, err, "all reference paths must exist before any profile is substituted")
	unknown := []byte(`{"category":"unknown","$profile":{"hash":"not-a-storage-reference"},"author":{"name":"Keep"}}`)
	payload, err := PrepareRetainedCapture("gallery-dl", "unknown", unknown)
	require.NoError(t, err)
	require.Empty(t, payload.Profiles)
	restored, err := RestoreCapture(payload)
	require.NoError(t, err)
	require.Equal(t, canonicalSourceFixture(t, unknown), restored)
}

func TestCaptureProfileExpansionIsBounded(t *testing.T) {
	body := json.RawMessage(`{"description":"` + strings.Repeat("x", MaxSourcePayloadBytes/8) + `"}`)
	profile := models.SourceProfileBody{Namespace: "native:twitter", Body: body, Hash: sourceProfileHash("native:twitter", body)}
	shared := make(sourceObject)
	payload := &models.SourceCapturePayload{Patch: json.RawMessage(`{}`), Profiles: []models.SourceProfileBody{profile}}
	for _, name := range strings.Fields("one two three four five six seven eight nine") {
		shared[name] = nil
		payload.Refs = append(payload.Refs, models.SourceProfileReference{Part: "shared", Path: "/" + name, Hash: profile.Hash})
	}
	var err error
	payload.Shared, err = EncodeSourceJSON(shared)
	require.NoError(t, err)
	_, err = RestoreCapture(payload)
	require.ErrorContains(t, err, "exceeds payload limit")
}
