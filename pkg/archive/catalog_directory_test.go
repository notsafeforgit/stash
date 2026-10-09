package archive_test

import (
	"testing"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stretchr/testify/require"
)

func TestCatalogDirectoryAccountTemplates(t *testing.T) {
	for _, tc := range []struct{ folder, namespace, handle, id string }{
		{"CuteLilAsya, reddit", "native:reddit", "cutelilasya", ""},
		{"ImElizabethTran, 742448640, twitter", "native:twitter", "imelizabethtran", "742448640"},
		{"Old_Name, twitter", "native:twitter", "old_name", ""},
		{"name.bsky.social, did:plc:abc123, bluesky", "native:bluesky", "name.bsky.social", "did:plc:abc123"},
		{"User.Name, instagram", "native:instagram", "user.name", ""},
		{"UserName, tiktok", "native:tiktok", "username", ""},
	} {
		refs := archive.CatalogDirectoryAccounts(tc.folder)
		require.NotEmpty(t, refs, tc.folder)
		require.Equal(t, tc.namespace, refs[0].Namespace)
		require.Equal(t, tc.handle, refs[0].Value)
		if tc.id != "" {
			require.Len(t, refs, 2)
			require.Equal(t, tc.id, refs[1].Value)
		}
	}
	for _, folder := range []string{"dvdngbyzr0 saved, reddit", "subreddits/example", "person, reddit/child", "person, numeric, twitter", "Display Name, reddit", "person, onlyfans", "manual", "../person, reddit", "person, 42, reddit"} {
		require.Empty(t, archive.CatalogDirectoryAccounts(folder), folder)
	}
	ref := archive.CatalogMirrorDirectoryAccount("yumae, onlyfans", "mirror:coomer:onlyfans")
	require.NotNil(t, ref)
	require.Equal(t, "catalog_label", ref.Kind)
	require.Nil(t, archive.CatalogMirrorDirectoryAccount("yumae, onlyfans", "native:onlyfans"))
	require.Nil(t, archive.CatalogMirrorDirectoryAccount("yumae, onlyfans", "mirror:kemono:patreon"))
}

func TestCatalogAccountReviewSeesHandleOnlyAuthors(t *testing.T) {
	raw := []byte(`{"category":"reddit","author":"DifferentAuthor"}`)
	identity, err := archive.ExtractCapturedAccount(raw)
	require.NoError(t, err)
	require.Nil(t, identity, "normal ingestion still requires an ID")
	identity, err = archive.CapturedAccountReferences(raw)
	require.NoError(t, err)
	require.NotNil(t, identity)
	require.Equal(t, "differentauthor", identity.Identifiers[0].Reference.Value)
}
