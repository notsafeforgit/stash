package scrape

import (
	"encoding/json"
	"html"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const DiscoveryIdentityPolicy = "retained-discovery-identity-v1"

// lookupValueLimit bounds one retained lookup row (historically one catalog chunk).
const lookupValueLimit = 16 << 20

var lookupCatalogID = regexp.MustCompile(`^c_[0-9a-f]{32}$`)

// lookupTarget is the subset of a historical finder target that identity
// matching needs. The finder's other statuses are no longer retained.
type lookupTarget struct {
	Platform, AccountKey, CandidateURL string
}

func lookupString(value any, allowEmpty bool, maxBytes int) (string, bool) {
	text, ok := value.(string)
	return text, ok && (allowEmpty || text != "") && len(text) <= maxBytes && !strings.ContainsRune(text, 0)
}

func lookupStringList(value any, maximum int) bool {
	items, ok := value.([]any)
	if !ok || len(items) > maximum {
		return false
	}
	for _, item := range items {
		if _, ok := lookupString(item, true, lookupValueLimit); !ok {
			return false
		}
	}
	return true
}

func lookupEvidence(value any) (map[string]any, bool) {
	raw, ok := value.(string)
	if !ok || raw == "" || len(raw) > lookupValueLimit {
		return nil, false
	}
	object, err := archive.DecodeJSONObject([]byte(raw), lookupValueLimit)
	return object, err == nil
}

// prepareLookupTarget validates a retained finder row exactly as the original
// automation import did for its "lookup" status.
func prepareLookupTarget(v map[string]any) (*lookupTarget, map[string]any, bool) {
	if catalog, ok := v["catalog_id"].(string); !ok || !lookupCatalogID.MatchString(catalog) {
		return nil, nil, false
	}
	if _, ok := lookupString(v["post_key"], false, 16384); !ok || v["job_key"] != nil {
		return nil, nil, false
	}
	if status, _ := v["status"].(string); status != "lookup" {
		return nil, nil, false
	}
	evidence, ok := lookupEvidence(v["evidence_json"])
	if !ok {
		return nil, nil, false
	}
	for _, key := range []string{"titles", "texts", "dates", "paths", "urls"} {
		if !lookupStringList(evidence[key], 65536) {
			return nil, nil, false
		}
	}
	if len(evidence["paths"].([]any)) == 0 {
		return nil, nil, false
	}
	r := &lookupTarget{}
	r.Platform, _ = evidence["platform"].(string)
	if r.Platform != "twitter" && r.Platform != "reddit" {
		return nil, nil, false
	}
	if evidence["account_key"] != nil {
		if r.AccountKey, ok = lookupString(evidence["account_key"], false, 8192); !ok {
			return nil, nil, false
		}
	}
	if _, ok := evidence["strict_filename_id"].(bool); !ok {
		return nil, nil, false
	}
	r.CandidateURL, ok = lookupString(evidence["candidate_url"], false, 8192)
	scope, err := SourceScopeV1(r.CandidateURL)
	if !ok || err != nil || !archive.EnrichmentPostURL(r.CandidateURL) || scope != "service:"+r.Platform {
		return nil, nil, false
	}
	return r, evidence, true
}

// LookupCandidateURL returns the retained finder's candidate post URL.
func LookupCandidateURL(values map[string]any) (string, bool) {
	target, _, ok := prepareLookupTarget(values)
	if !ok {
		return "", false
	}
	return target.CandidateURL, true
}

var discoveryMarkup = regexp.MustCompile(`<[^>]+>`)
var discoveryTwitterFilename = regexp.MustCompile(`^([0-9]{15,20})_[0-9]{1,3}$`)

// MatchDiscoveryLookup compares source metadata with a retained lookup's
// corroborating evidence. A filename URL identifies a candidate to fetch; it
// does not by itself establish which historical post owns that source ID.
// This version covers the Reddit/Twitter formats the historical finder wrote.
func MatchDiscoveryLookup(values map[string]any, raw json.RawMessage) (*models.SourcePostIdentifier, string, error) {
	prepared, evidence, ok := prepareLookupTarget(values)
	if !ok {
		return nil, "", models.ErrEnrichmentInvalid
	}
	post, err := archive.ExtractCapturedPost(raw)
	if err != nil {
		return nil, "", err
	}
	candidate := discoveryURLIdentity(prepared.CandidateURL, prepared.Platform)
	if post == nil || candidate == nil || *post != *candidate {
		return nil, "", nil
	}
	if strict, _ := evidence["strict_filename_id"].(bool); strict && prepared.Platform == "twitter" {
		// Recheck the historical flag against every saved path. A changed or
		// malformed flag cannot promote an unrelated file prefix to identity.
		strictPaths := true
		for _, item := range evidence["paths"].([]any) {
			name := path.Base(item.(string))
			match := discoveryTwitterFilename.FindStringSubmatch(strings.TrimSuffix(name, path.Ext(name)))
			if len(match) != 2 || match[1] != candidate.Value {
				strictPaths = false
				break
			}
		}
		if strictPaths {
			return post, "strict-filename-id", nil
		}
	}
	account, err := archive.ExtractCapturedAccount(raw)
	if err != nil {
		return nil, "", err
	}
	parts := strings.SplitN(prepared.AccountKey, ":", 3)
	if len(parts) == 3 && parts[0] == prepared.Platform && account != nil && account.Namespace == post.Namespace {
		ref, err := archive.NormalizeAccountReference(models.AccountReference{Namespace: post.Namespace, Kind: parts[1], Value: parts[2]})
		if err == nil && (ref.Kind == "id" || ref.Kind == "handle") {
			for _, captured := range account.Identifiers {
				if captured.Reference == ref {
					return post, "captured-account-and-post-id", nil
				}
			}
		}
	}
	metadata, err := archive.CapturedMetadata(raw)
	if err != nil {
		return nil, "", err
	}
	day := discoverySourceDay(metadata.PublishedAt)
	if day == "" || !discoveryContains(evidence["dates"], day) {
		return nil, "", nil
	}
	for _, field := range []struct {
		value *string
		list  string
		min   int
		basis string
	}{
		{metadata.OriginalText, "texts", 40, "original-text-date-and-post-id"},
		{metadata.Title, "titles", 20, "title-date-and-post-id"},
	} {
		if field.value != nil {
			words := discoveryWords(*field.value)
			if utf8.RuneCountInString(words) >= field.min && discoveryContains(evidence[field.list], words) {
				return post, field.basis, nil
			}
		}
	}
	return nil, "", nil
}

func discoveryURLIdentity(raw, platform string) *models.SourcePostIdentifier {
	if !archive.EnrichmentPostURL(raw) {
		return nil
	}
	scope, err := SourceScopeV1(raw)
	if err != nil || scope != "service:"+platform {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var id string
	for i, part := range parts {
		if i+1 < len(parts) && ((platform == "twitter" && part == "status") || (platform == "reddit" && (part == "comments" || part == "gallery"))) {
			id = parts[i+1]
			break
		}
	}
	if platform == "reddit" {
		if u.Hostname() == "redd.it" && len(parts) == 1 {
			id = parts[0]
		}
		id = strings.ToLower(id)
	}
	if id == "" {
		return nil
	}
	return &models.SourcePostIdentifier{Namespace: "native:" + platform, Value: id}
}

func discoveryContains(values any, value string) bool {
	items, ok := values.([]any)
	return ok && slices.Contains(items, any(value))
}

// Preserve the old finder's HTML/NFKC/case-folded Unicode word comparison.
// Date alone, translated text and approximate caption similarity are not proof.
func discoveryWords(value string) string {
	value = html.UnescapeString(discoveryMarkup.ReplaceAllString(value, " "))
	value = cases.Fold().String(norm.NFKC.String(value))
	return strings.Join(strings.FieldsFunc(value, func(r rune) bool {
		return r != '_' && !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}), " ")
}

func discoverySourceDay(value *string) string {
	if value == nil {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999", time.DateOnly} {
		if at, err := time.Parse(layout, *value); err == nil {
			return at.UTC().Format(time.DateOnly)
		}
	}
	return ""
}
