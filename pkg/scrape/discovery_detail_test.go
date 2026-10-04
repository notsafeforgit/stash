package scrape_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

const detailTitle = "a distinctive original album title"

func detailFixture(t *testing.T, platform string) (map[string]any, map[string]any, map[string]any, models.SourcePostIdentifier) {
	t.Helper()
	values := listingEvidence(t, platform, func(e map[string]any) {
		e["titles"], e["dates"] = []string{detailTitle}, []string{"2025-01-02"}
		e["strict_filename_id"], e["paths"] = true, []string{"123456789012345_1.jpg"}
	})
	selected := models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}
	profile, url := "https://www.reddit.com/user/juniper/submitted/", "https://www.reddit.com/comments/abc123"
	metadata := map[string]any{"category": "reddit", "id": "abc123", "author": "juniper", "author_fullname": "t2_abc", "title": detailTitle}
	if platform == "twitter" {
		selected = models.SourcePostIdentifier{Namespace: "native:twitter", Value: "123456789012345"}
		profile, url = "https://x.com/id:123/timeline", "https://x.com/i/web/status/123456789012345"
		metadata = map[string]any{"category": "twitter", "tweet_id": selected.Value, "author": map[string]any{"id": "123", "name": "juniper"}, "title": detailTitle}
	}
	metadata["source_extractor_url"] = profile
	record := func(data map[string]any) map[string]any {
		return map[string]any{"kind": "post", "base": nil, "parent": nil, "patch": data, "removed": []any{}, "observed_at": "2026-10-04T01:02:03.000000004Z"}
	}
	page := map[string]any{"schema": archive.DiscoveryPageSchema, "url": profile, "retention_policy": archive.SourceRetentionVersion,
		"extractor_version": "listing-runtime", "cursor": nil, "next_cursor": nil, "complete": true, "records": []any{record(metadata)}}
	copyData := map[string]any{}
	for key, value := range metadata {
		copyData[key] = value
	}
	copyData["date"], copyData["source_extractor_url"] = "2025-01-02", url
	detail := map[string]any{"schema": archive.EnrichmentTranscriptSchema, "url": url, "retention_policy": archive.SourceRetentionVersion,
		"extractor_version": "detail-runtime", "records": []any{record(copyData)}, "pending": []any{}, "unresolved": []any{}}
	return values, page, detail, selected
}

func detailJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	body, err := archive.EncodeSourceJSON(value)
	require.NoError(t, err)
	return body
}

func detailPatch(detail map[string]any) map[string]any {
	return detail["records"].([]any)[0].(map[string]any)["patch"].(map[string]any)
}

func TestDiscoveryDetailCorroboratesOriginalEvidenceAndKeepsReferences(t *testing.T) {
	for _, platform := range []string{"reddit", "twitter"} {
		t.Run(platform, func(t *testing.T) {
			values, page, detail, selected := detailFixture(t, platform)
			before := detailJSON(t, values)
			pageBody, detailBody := detailJSON(t, page), detailJSON(t, detail)
			matches, err := scrape.MatchDiscoveryPage(values, pageBody)
			require.NoError(t, err)
			require.True(t, matches[0].NeedsDetail)
			result, err := scrape.MatchDiscoveryDetail(values, pageBody, selected, "detail-runtime", detailBody)
			require.NoError(t, err)
			require.Equal(t, "corroborated", result.Status)
			require.Equal(t, scrape.DiscoveryDetailMatchPolicy, result.Policy)
			require.Equal(t, selected, result.Post)
			require.Equal(t, "exact-title-and-date", result.Basis)
			require.Equal(t, 0, *result.WitnessOrdinal)
			require.Equal(t, []int{0}, result.RecordOrdinals)
			require.Equal(t, scrape.CatalogSnapshotSHA(pageBody), result.PageSHA256)
			require.Equal(t, scrape.CatalogSnapshotSHA(detailBody), result.TranscriptSHA256)
			replay, err := scrape.MatchDiscoveryDetail(values, pageBody, selected, "detail-runtime", detailBody)
			require.NoError(t, err)
			require.Equal(t, result, replay)
			require.Equal(t, before, detailJSON(t, values), "the inferred URL must not be added to original evidence")
		})
	}
}

func TestDiscoveryDetailDoesNotManufactureCorroboration(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"title only", func(d map[string]any) { delete(detailPatch(d), "date") }},
		{"wrong day", func(d map[string]any) { detailPatch(d)["date"] = "2025-01-03" }},
		{"account and inferred ID only", func(d map[string]any) { delete(detailPatch(d), "title") }},
		{"empty response", func(d map[string]any) { d["records"] = []any{} }},
		{"capture time is not publication date", func(d map[string]any) {
			delete(detailPatch(d), "date")
			d["records"].([]any)[0].(map[string]any)["observed_at"] = "2025-01-02T00:00:00Z"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			values, page, detail, selected := detailFixture(t, "twitter")
			test.change(detail)
			result, err := scrape.MatchDiscoveryDetail(values, detailJSON(t, page), selected, "detail-runtime", detailJSON(t, detail))
			require.NoError(t, err)
			require.Equal(t, "uncorroborated", result.Status)
			require.Nil(t, result.WitnessOrdinal)
			require.Empty(t, result.Basis)
		})
	}
}

func TestDiscoveryDetailRejectsChangedSelectionOrForeignEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(map[string]any, map[string]any, *models.SourcePostIdentifier)
	}{
		{"candidate not on saved page", func(_, _ map[string]any, selected *models.SourcePostIdentifier) { selected.Value = "999" }},
		{"already strong candidate", func(p, _ map[string]any, _ *models.SourcePostIdentifier) { detailPatch(p)["date"] = "2025-01-02" }},
		{"changed fetch URL", func(_, d map[string]any, _ *models.SourcePostIdentifier) { d["url"] = "https://x.com/i/web/status/999" }},
		{"changed runtime", func(_, d map[string]any, _ *models.SourcePostIdentifier) {
			d["extractor_version"] = "unexpected-runtime"
		}},
		{"wrong returned post", func(_, d map[string]any, _ *models.SourcePostIdentifier) { detailPatch(d)["tweet_id"] = "999" }},
		{"contradictory publisher", func(_, d map[string]any, _ *models.SourcePostIdentifier) {
			detailPatch(d)["author"] = map[string]any{"id": "999", "name": "juniper"}
		}},
		{"publisher conflict in later attachment", func(_, d map[string]any, _ *models.SourcePostIdentifier) {
			d["records"] = append(d["records"].([]any), map[string]any{"kind": "media", "base": 0, "parent": nil,
				"patch":   map[string]any{"author": map[string]any{"id": "999", "name": "juniper"}, "num": 2},
				"removed": []any{}, "observed_at": "2026-10-04T01:02:04Z"})
		}},
		{"another post in later attachment", func(_, d map[string]any, _ *models.SourcePostIdentifier) {
			d["records"] = append(d["records"].([]any), map[string]any{"kind": "media", "base": 0, "parent": nil,
				"patch": map[string]any{"tweet_id": "999"}, "removed": []any{}, "observed_at": "2026-10-04T01:02:04Z"})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			values, page, detail, selected := detailFixture(t, "twitter")
			test.change(page, detail, &selected)
			result, err := scrape.MatchDiscoveryDetail(values, detailJSON(t, page), selected, "detail-runtime", detailJSON(t, detail))
			require.ErrorIs(t, err, models.ErrDiscoveryConflict)
			require.Nil(t, result)
		})
	}
}

func TestDiscoveryDetailKeepsPendingAndUnresolvedChildrenDistinct(t *testing.T) {
	values, page, detail, selected := detailFixture(t, "reddit")
	reference := map[string]any{"url": "https://www.redgifs.com/ifr/child", "parent": 0, "depth": 1, "reason": "authentication"}
	detail["pending"] = []any{reference}
	result, err := scrape.MatchDiscoveryDetail(values, detailJSON(t, page), selected, "detail-runtime", detailJSON(t, detail))
	require.NoError(t, err)
	require.Equal(t, "pending", result.Status)
	require.Equal(t, 1, result.PendingCount)
	require.Nil(t, result.WitnessOrdinal)
	detail["pending"] = []any{}
	reference["reason"] = "external_reference_only"
	detail["unresolved"] = []any{reference}
	result, err = scrape.MatchDiscoveryDetail(values, detailJSON(t, page), selected, "detail-runtime", detailJSON(t, detail))
	require.NoError(t, err)
	require.Equal(t, "corroborated", result.Status)
	require.Equal(t, 1, result.UnresolvedCount)
	require.Equal(t, []int{0}, result.RecordOrdinals)
}

func TestDiscoveryDetailCannotRemoveCompetingCandidates(t *testing.T) {
	values, page, detail, selected := detailFixture(t, "twitter")
	page["records"] = append(page["records"].([]any), map[string]any{"kind": "post", "base": 0, "parent": nil,
		"patch": map[string]any{"tweet_id": "999"}, "removed": []any{}, "observed_at": "2026-10-04T01:02:04Z"})
	body := detailJSON(t, page)
	before, err := scrape.MatchDiscoveryPage(values, body)
	require.NoError(t, err)
	require.Len(t, before, 2)
	verified, err := scrape.MatchDiscoveryDetail(values, body, selected, "detail-runtime", detailJSON(t, detail))
	require.NoError(t, err)
	require.Equal(t, "corroborated", verified.Status)
	selected.Value = "999"
	detailPatch(detail)["tweet_id"], detail["url"] = "999", "https://x.com/i/web/status/999"
	detailPatch(detail)["title"] = strings.Repeat("unrelated ", 8)
	other, err := scrape.MatchDiscoveryDetail(values, body, selected, "detail-runtime", detailJSON(t, detail))
	require.NoError(t, err)
	require.Equal(t, "uncorroborated", other.Status)
	after, err := scrape.MatchDiscoveryPage(values, body)
	require.NoError(t, err)
	require.Equal(t, before, after, "a failed or changed detail is not proof to erase an earlier candidate")
}

func TestDiscoveryDetailCannotSupplyItsOwnOriginalCorroboration(t *testing.T) {
	_, page, detail, selected := detailFixture(t, "twitter")
	values := listingEvidence(t, "twitter", func(e map[string]any) {
		e["titles"], e["dates"] = []string{detailTitle}, []string{}
		e["strict_filename_id"], e["paths"] = true, []string{"123456789012345_1.jpg"}
	})
	result, err := scrape.MatchDiscoveryDetail(values, detailJSON(t, page), selected, "detail-runtime", detailJSON(t, detail))
	require.NoError(t, err)
	require.Equal(t, "uncorroborated", result.Status, "a newly fetched date was never part of the original evidence")
	require.Nil(t, result.WitnessOrdinal)
	detailPatch(detail)["title"] = strings.Repeat("x", 1<<19)
	result, err = scrape.MatchDiscoveryDetail(values, detailJSON(t, page), selected, "detail-runtime", detailJSON(t, detail))
	require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
	require.Nil(t, result)
}
