package archive

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/models"
)

const (
	DiscoveryPageSchema   = "stash-discovery-page-v1"
	MaxDiscoveryRecords   = 4096
	MaxDiscoveryPageBytes = MaxEnrichmentTranscriptBytes
)

var (
	discoveryTwitterPath = regexp.MustCompile(`^/(?:id:[0-9]+|[A-Za-z0-9_]{1,30})/timeline/?$`)
	discoveryRedditPath  = regexp.MustCompile(`^/user/[A-Za-z0-9_-]{1,40}/submitted/$`)
	discoveryRedditAfter = regexp.MustCompile(`^t3_[a-z0-9]+$`)
)

// DiscoveryPage is unaccepted listing evidence. Parsing neither binds a page to
// a job nor proves an account/post identity. The coordinator must retain its
// original request, owned attempt and cursor atomically before advancing work.
type DiscoveryPage struct {
	Schema           string             `json:"schema"`
	URL              string             `json:"url"`
	RetentionPolicy  string             `json:"retention_policy"`
	ExtractorVersion string             `json:"extractor_version"`
	Cursor           map[string]string  `json:"cursor"`
	NextCursor       map[string]string  `json:"next_cursor"`
	Complete         bool               `json:"complete"`
	Records          []EnrichmentRecord `json:"records"`
	body             json.RawMessage
	transcript       EnrichmentTranscript
}

// DiscoveryProfilePlatform restricts listing jobs to the producer's supported
// profile forms. In particular, a feed cursor never belongs in the profile URL.
func DiscoveryProfilePlatform(value string) (string, error) {
	if !utf8.ValidString(value) || !strings.HasPrefix(value, "https://") || !enrichmentPublicURL(value) {
		return "", models.ErrDiscoveryInvalid
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host != u.Hostname() || u.Port() != "" || u.Fragment != "" {
		return "", models.ErrDiscoveryInvalid
	}
	switch strings.ToLower(u.Hostname()) {
	case "x.com", "www.x.com", "twitter.com", "www.twitter.com":
		if discoveryTwitterPath.MatchString(u.EscapedPath()) && u.RawQuery == "" {
			return "twitter", nil
		}
	case "reddit.com", "www.reddit.com":
		query, err := url.ParseQuery(u.RawQuery)
		if err == nil && discoveryRedditPath.MatchString(u.EscapedPath()) &&
			(len(query) == 0 || (len(query) == 1 && len(query["sort"]) == 1 && query.Get("sort") == "new")) {
			return "reddit", nil
		}
	}
	return "", models.ErrDiscoveryInvalid
}

func discoveryCursor(platform string, raw any) (map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	key := "cursor"
	if platform == "reddit" {
		key = "after"
	}
	value, ok := raw.(sourceObject)
	if !ok || !enrichmentObjectKeys(value, key) {
		return nil, models.ErrDiscoveryInvalid
	}
	token, ok := value[key].(string)
	if !ok || !utf8.ValidString(token) || len(token) == 0 || len(token) > 8192 ||
		strings.ContainsFunc(token, func(r rune) bool { return r < 32 || r == 127 }) ||
		(platform == "reddit" && !discoveryRedditAfter.MatchString(token)) {
		return nil, models.ErrDiscoveryInvalid
	}
	return map[string]string{key: token}, nil
}

func ParseDiscoveryPage(raw []byte) (*DiscoveryPage, error) {
	value, err := DecodeJSONObject(raw, MaxDiscoveryPageBytes)
	if err != nil || !enrichmentObjectKeys(value, "schema", "url", "retention_policy", "extractor_version", "cursor", "next_cursor", "complete", "records") {
		return nil, models.ErrDiscoveryInvalid
	}
	p := &DiscoveryPage{transcript: EnrichmentTranscript{
		Schema: EnrichmentTranscriptSchema, Records: []EnrichmentRecord{}, seen: map[string]bool{},
	}}
	p.Schema, _ = value["schema"].(string)
	p.URL, _ = value["url"].(string)
	p.RetentionPolicy, _ = value["retention_policy"].(string)
	p.ExtractorVersion, _ = value["extractor_version"].(string)
	platform, err := DiscoveryProfilePlatform(p.URL)
	if err != nil || p.Schema != DiscoveryPageSchema || p.RetentionPolicy != SourceRetentionVersion ||
		p.ExtractorVersion == "" || len(p.ExtractorVersion) > 128 || strings.ContainsAny(p.ExtractorVersion, "\r\n\x00") {
		return nil, models.ErrDiscoveryInvalid
	}
	p.Cursor, err = discoveryCursor(platform, value["cursor"])
	if err != nil {
		return nil, err
	}
	p.NextCursor, err = discoveryCursor(platform, value["next_cursor"])
	if err != nil {
		return nil, err
	}
	var ok bool
	p.Complete, ok = value["complete"].(bool)
	if !ok || (p.Complete && p.NextCursor != nil) || (!p.Complete && (p.NextCursor == nil || maps.Equal(p.Cursor, p.NextCursor))) {
		return nil, models.ErrDiscoveryInvalid
	}
	records, ok := value["records"].([]any)
	if !ok || len(records) > MaxDiscoveryRecords {
		return nil, models.ErrDiscoveryInvalid
	}
	expandedBytes := 0
	for _, record := range records {
		if err := p.transcript.restore(record, &expandedBytes); err != nil {
			return nil, models.ErrDiscoveryInvalid
		}
	}
	p.Records = p.transcript.Records
	p.body, err = EncodeSourceJSON(value)
	if err != nil || len(p.body) > MaxDiscoveryPageBytes {
		return nil, models.ErrDiscoveryInvalid
	}
	return p, nil
}

func (p *DiscoveryPage) Body() json.RawMessage { return bytes.Clone(p.body) }

func (p *DiscoveryPage) Metadata(index int) (json.RawMessage, error) {
	body, err := p.transcript.Metadata(index)
	if err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	return body, nil
}
