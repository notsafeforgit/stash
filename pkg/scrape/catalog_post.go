package scrape

import (
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type CatalogPostIdentity struct {
	Legacy     models.SourcePostIdentifier
	Identifier models.SourcePostIdentifier
	Basis      string
}

var catalogRedditPostID = regexp.MustCompile(`^[0-9a-z]{1,30}$`)
var catalogNumericPostID = regexp.MustCompile(`^[0-9]{1,30}$`)
var catalogBlueskyPostID = regexp.MustCompile(`^did:(?:plc:[A-Za-z0-9]+|web:[A-Za-z0-9.:-]+)/[A-Za-z0-9._~-]+$`)

// CatalogLocalPostReference scopes original keys and aliases to one physical
// catalog. Their spelling alone never proves a global service post identity.
func CatalogLocalPostReference(source, catalog, key string) (models.SourcePostIdentifier, error) {
	if !catalogIdentityUUID(source) || !catalogSnapshotID.MatchString(catalog) || key == "" {
		return models.SourcePostIdentifier{}, models.ErrCatalogSnapshotInvalid
	}
	body, err := LegacyCatalogJSON([]any{catalog, key}, 65536)
	if err != nil {
		return models.SourcePostIdentifier{}, err
	}
	return models.SourcePostIdentifier{Namespace: "legacy:catalog:" + source, Value: "post:" + CatalogSnapshotSHA(body)}, nil
}

// CatalogPostReference keeps mirror identities qualified even though old post
// rows call their platform "onlyfans"/"patreon". Unqualified, local, or
// conflicting identities remain scoped to the physical source catalog.
func CatalogPostReference(source, catalog string, row map[string]any, urls []string) (*CatalogPostIdentity, error) {
	key, keyOK := row["post_key"].(string)
	platform, platformOK := row["platform"].(string)
	basis, basisOK := row["identity_basis"].(string)
	if !catalogIdentityUUID(source) || !catalogSnapshotID.MatchString(catalog) || !keyOK || key == "" || !platformOK || platform == "" || !basisOK {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	legacy, err := CatalogLocalPostReference(source, catalog, key)
	if err != nil {
		return nil, err
	}
	ret := &CatalogPostIdentity{Legacy: legacy, Identifier: legacy, Basis: "catalog_local_identity"}
	id, _ := row["source_id"].(string)
	if row["source_id"] != nil && id == "" {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	if id == "" || (basis != "source-id" && basis != "source-url" && basis != "verified-enrichment-url") {
		return ret, nil
	}
	namespaces := map[string]bool{}
	for _, raw := range urls {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			continue
		}
		path := strings.TrimSuffix(parsed.EscapedPath(), "/")
		separator := strings.LastIndex(path, "/post/")
		if separator < 0 {
			continue
		}
		post := path[separator+6:]
		parsed.Path, parsed.RawPath, parsed.RawQuery, parsed.Fragment = path[:separator], "", "", ""
		profile := archive.ProfileReference(parsed.String())
		if profile != nil && strings.HasPrefix(profile.Namespace, "mirror:") && strings.HasSuffix(profile.Namespace, ":"+platform) && profile.Value+"/"+post == id {
			namespaces[profile.Namespace] = true
		}
	}
	if len(namespaces) > 1 {
		ret.Basis = "conflicting_mirror_identity"
		return ret, nil
	}
	if len(namespaces) == 1 {
		for namespace := range namespaces {
			ret.Identifier = models.SourcePostIdentifier{Namespace: namespace, Value: id}
			ret.Basis = "catalog_mirror_post"
		}
	} else {
		valid := false
		switch platform {
		case "reddit":
			valid = catalogRedditPostID.MatchString(id)
		case "twitter", "instagram", "tiktok", "tumblr":
			valid = catalogNumericPostID.MatchString(id)
		case "bluesky":
			valid = catalogBlueskyPostID.MatchString(id)
		}
		if valid {
			ret.Identifier = models.SourcePostIdentifier{Namespace: "native:" + platform, Value: id}
			ret.Basis = "catalog_source_id"
		}
	}
	if _, err := archive.NormalizeAccountReference(models.AccountReference{Namespace: ret.Identifier.Namespace, Kind: "id", Value: ret.Identifier.Value}); err != nil {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	return ret, nil
}

func CatalogEvidenceTables() []string {
	names := []string{"account_snapshots", "observations", "observation_details", "posts"}
	sort.Strings(names)
	return names
}
