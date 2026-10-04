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

var discoveryMarkup = regexp.MustCompile(`<[^>]+>`)
var discoveryTwitterFilename = regexp.MustCompile(`^([0-9]{15,20})_[0-9]{1,3}$`)

// MatchDiscoveryLookup compares source metadata with a retained lookup's
// corroborating evidence. A filename URL identifies a candidate to fetch; it
// does not by itself establish which historical post owns that source ID.
// This version covers the Reddit/Twitter formats the historical finder wrote.
func MatchDiscoveryLookup(values map[string]any, raw json.RawMessage) (*models.SourcePostIdentifier, string, error) {
	prepared, reason := prepareDiscoveryTarget(values)
	if reason != "" || prepared.Status != "lookup" {
		return nil, "", models.ErrEnrichmentInvalid
	}
	evidence, _, valid := discoveryObject(values["evidence_json"], CatalogChunkLimit)
	if !valid {
		return nil, "", models.ErrEnrichmentInvalid
	}
	post, err := archive.ExtractCapturedPost(raw)
	if err != nil {
		return nil, "", err
	}
	candidate := discoveryURLIdentity(prepared.Projection.CandidateURL, prepared.Platform)
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
