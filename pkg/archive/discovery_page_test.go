package archive

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

type discoveryPageFixture struct {
	Pages []struct {
		Name     string            `json:"name"`
		Page     json.RawMessage   `json:"page"`
		Metadata []json.RawMessage `json:"metadata"`
	} `json:"pages"`
	Profiles []struct {
		URL      string `json:"url"`
		Platform string `json:"platform"`
	} `json:"profiles"`
	Rejected []struct {
		Name   string          `json:"name"`
		Path   []any           `json:"path"`
		Remove bool            `json:"remove"`
		Value  json.RawMessage `json:"value"`
	} `json:"rejected"`
}

func readDiscoveryPageFixture(t *testing.T) discoveryPageFixture {
	t.Helper()
	body, err := os.ReadFile("testdata/discovery-pages-v1.json")
	require.NoError(t, err)
	var fixture discoveryPageFixture
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&fixture))
	return fixture
}

func TestDiscoveryPageReconstructsSharedProducerEvidence(t *testing.T) {
	for _, test := range readDiscoveryPageFixture(t).Pages {
		t.Run(test.Name, func(t *testing.T) {
			page, err := ParseDiscoveryPage(test.Page)
			require.NoError(t, err)
			require.Len(t, page.Records, len(test.Metadata))
			for i, raw := range test.Metadata {
				value, err := DecodeJSONObject(raw, MaxSourcePayloadBytes)
				require.NoError(t, err)
				expected, err := EncodeSourceJSON(value)
				require.NoError(t, err)
				actual, err := page.Metadata(i)
				require.NoError(t, err)
				require.Equal(t, expected, actual)
			}
			replayed, err := ParseDiscoveryPage(page.Body())
			require.NoError(t, err)
			require.Equal(t, page.Body(), replayed.Body())
			require.Equal(t, page.Records, replayed.Records)
			body := page.Body()
			body[0] = '['
			require.NotEqual(t, body, page.Body(), "caller cannot rewrite retained bytes")
			_, err = page.Metadata(-1)
			require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
			_, err = page.Metadata(len(page.Records))
			require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
			_, err = ParseEnrichmentTranscript(page.Body())
			require.ErrorIs(t, err, models.ErrEnrichmentInvalid, "listing is not a post checkpoint")
		})
	}
}

func TestDiscoveryProfileContract(t *testing.T) {
	for _, test := range readDiscoveryPageFixture(t).Profiles {
		t.Run(test.URL, func(t *testing.T) {
			platform, err := DiscoveryProfilePlatform(test.URL)
			if test.Platform == "" {
				require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.Platform, platform)
			}
		})
	}
	_, err := DiscoveryProfilePlatform("https://x.com/" + string([]byte{0xff}) + "/timeline")
	require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
}

func TestDiscoveryCursorBindsItsServiceAndUTF8ByteLimit(t *testing.T) {
	for _, platform := range []string{"twitter", "reddit"} {
		cursor, err := discoveryCursor(platform, nil)
		require.NoError(t, err)
		require.Nil(t, cursor)
	}
	valid := strings.Repeat("é", 4096)
	cursor, err := discoveryCursor("twitter", sourceObject{"cursor": valid})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"cursor": valid}, cursor)
	cursor, err = discoveryCursor("reddit", sourceObject{"after": "t3_abc123"})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"after": "t3_abc123"}, cursor)
	for _, test := range []struct {
		platform string
		cursor   sourceObject
	}{
		{"twitter", sourceObject{"cursor": valid + "é"}},
		{"twitter", sourceObject{"cursor": string([]byte{0xff})}},
		{"twitter", sourceObject{"cursor": "contains\x7fcontrol"}},
		{"reddit", sourceObject{"after": "t3_ABC"}},
		{"reddit", sourceObject{"after": "t3_valid", "cursor": "extra"}},
		{"reddit", sourceObject{"cursor": "1/saved"}},
	} {
		_, err := discoveryCursor(test.platform, test.cursor)
		require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
	}
}

func TestDiscoveryPageRejectsMalformedAndUnretainedEvidence(t *testing.T) {
	fixture := readDiscoveryPageFixture(t)
	for _, test := range fixture.Rejected {
		t.Run(test.Name, func(t *testing.T) {
			value, err := DecodeJSONObject(fixture.Pages[0].Page, MaxDiscoveryPageBytes)
			require.NoError(t, err)
			var parent any = value
			for _, part := range test.Path[:len(test.Path)-1] {
				switch key := part.(type) {
				case string:
					parent = parent.(sourceObject)[key]
				case json.Number:
					i, err := strconv.Atoi(string(key))
					require.NoError(t, err)
					parent = parent.([]any)[i]
				default:
					t.Fatalf("unexpected fixture path segment %T", part)
				}
			}
			key := test.Path[len(test.Path)-1].(string)
			if test.Remove {
				delete(parent.(sourceObject), key)
			} else {
				replacement, err := DecodeJSONObject(append(append([]byte(`{"value":`), test.Value...), '}'), MaxDiscoveryPageBytes)
				require.NoError(t, err)
				parent.(sourceObject)[key] = replacement["value"]
			}
			body, err := EncodeSourceJSON(value)
			require.NoError(t, err)
			_, err = ParseDiscoveryPage(body)
			require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
		})
	}
	for name, raw := range map[string][]byte{
		"duplicate keys":  append([]byte(`{"cursor":null,`), fixture.Pages[0].Page[1:]...),
		"invalid unicode": []byte(`{"url":"https://x.com/\ud800/timeline"}`),
		"trailing JSON":   append(bytes.Clone(fixture.Pages[0].Page), []byte(`{}`)...),
		"byte limit":      bytes.Repeat([]byte{' '}, MaxDiscoveryPageBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseDiscoveryPage(raw)
			require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
		})
	}
}

func TestDiscoveryPageRecordAndExpansionBounds(t *testing.T) {
	value, err := DecodeJSONObject(readDiscoveryPageFixture(t).Pages[0].Page, MaxDiscoveryPageBytes)
	require.NoError(t, err)
	first := value["records"].([]any)[0].(sourceObject)
	records := []any{first}
	for i := 1; i < MaxDiscoveryRecords; i++ {
		records = append(records, sourceObject{"kind": "media", "base": 0, "parent": nil,
			"patch": sourceObject{"num": i}, "removed": []any{}, "observed_at": first["observed_at"]})
	}
	parse := func() (*DiscoveryPage, error) {
		value["records"] = records
		body, err := EncodeSourceJSON(value)
		require.NoError(t, err)
		return ParseDiscoveryPage(body)
	}
	page, err := parse()
	require.NoError(t, err)
	require.Len(t, page.Records, MaxDiscoveryRecords)
	records = append(records, first)
	_, err = parse()
	require.ErrorIs(t, err, models.ErrDiscoveryInvalid)

	// Compact deltas must not circumvent the shared 128 MiB expansion limit.
	records = records[:65]
	first["patch"].(sourceObject)["large_text"] = strings.Repeat("x", 2<<20)
	_, err = parse()
	require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
	delete(first["patch"].(sourceObject), "large_text")
	records = records[:4]
	for i := 1; i < len(records); i++ {
		records[i].(sourceObject)["parent"] = i - 1
	}
	_, err = parse()
	require.ErrorIs(t, err, models.ErrDiscoveryInvalid, "parent nesting remains bounded")
}
