package scrape_test

import (
	"testing"

	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

func TestCatalogPostReferenceQualifiesServicesAndMirrorEvidence(t *testing.T) {
	for _, test := range []struct {
		platform, id, basis, url, namespace string
	}{
		{"reddit", "abc123", "source-id", "", "native:reddit"},
		{"twitter", "9007199254740993", "source-url", "", "native:twitter"},
		{"instagram", "123456", "verified-enrichment-url", "", "native:instagram"},
		{"tiktok", "98765", "source-id", "", "native:tiktok"},
		{"bluesky", "did:plc:abcdef/3abcd", "source-url", "", "native:bluesky"},
		{"onlyfans", "account/123", "source-url", "https://coomer.st/onlyfans/user/account/post/123", "mirror:coomer:onlyfans"},
		{"patreon", "456/789", "source-url", "https://kemono.cr/patreon/user/456/post/789", "mirror:kemono:patreon"},
		{"fansly", "456/789", "source-url", "https://coomer.su/fansly/user/456/post/789", "mirror:coomer:fansly"},
		{"onlyfans", "account/123", "source-id", "", ""},
		{"patreon", "456/789", "source-url", "https://elsewhere.example/patreon/user/456/post/789", ""},
		{"reddit", "abc123", "local-sidecar", "", ""},
		{"twitter", "not-an-id", "source-id", "", ""},
	} {
		t.Run(test.platform+"/"+test.id+"/"+test.basis+"/"+test.namespace, func(t *testing.T) {
			row := map[string]any{"post_key": "old-key", "platform": test.platform, "source_id": test.id, "identity_basis": test.basis}
			identity, err := scrape.CatalogPostReference("11111111-1111-4111-8111-111111111111", "c_11111111111111111111111111111111", row, []string{test.url})
			require.NoError(t, err)
			if test.namespace == "" {
				require.Equal(t, identity.Legacy, identity.Identifier)
			} else {
				require.Equal(t, test.namespace, identity.Identifier.Namespace)
				require.Equal(t, test.id, identity.Identifier.Value)
			}
		})
	}
}

func TestCatalogPostLocalKeysCannotUnifyDifferentCatalogs(t *testing.T) {
	row := map[string]any{"post_key": "legacy:post:filename", "platform": "unknown", "source_id": nil, "identity_basis": "local-sidecar"}
	one, err := scrape.CatalogPostReference("11111111-1111-4111-8111-111111111111", "c_11111111111111111111111111111111", row, nil)
	require.NoError(t, err)
	two, err := scrape.CatalogPostReference("11111111-1111-4111-8111-111111111111", "c_22222222222222222222222222222222", row, nil)
	require.NoError(t, err)
	require.NotEqual(t, one.Identifier.Value, two.Identifier.Value)
	row["platform"], row["source_id"], row["identity_basis"] = "patreon", "456/789", "source-url"
	conflict, err := scrape.CatalogPostReference("11111111-1111-4111-8111-111111111111", "c_11111111111111111111111111111111", row, []string{"https://coomer.st/patreon/user/456/post/789", "https://kemono.cr/patreon/user/456/post/789"})
	require.NoError(t, err)
	require.Equal(t, "conflicting_mirror_identity", conflict.Basis)
	require.Equal(t, conflict.Legacy, conflict.Identifier)
}
