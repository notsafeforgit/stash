package archive

import (
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestAccountReferencesNormalizeOnlyKnownNativeHandles(t *testing.T) {
	for _, tc := range []struct{ namespace, kind, value, want string }{
		{"native:reddit", "handle", "Alice", "alice"},
		{"native:instagram", "handle", "Alice.Name", "alice.name"},
		{"native:twitter", "handle", "CAFÉ", "café"},
		{"native:bluesky", "id", "did:web:Account.test", "did:web:Account.test"},
		{"native:tiktok", "secUid", "CaseSensitive", "CaseSensitive"},
		{"native:onlyfans", "id", "CaseSensitive", "CaseSensitive"},
		{"mirror:coomer:onlyfans", "user", "CaseSensitive", "CaseSensitive"},
		{"native:unknown-extractor", "handle", "CaseSensitive", "CaseSensitive"},
		{"url", "profile", "http://www.example.test/Case?account=70#presentation", "https://example.test/Case?account=70"},
	} {
		ref, err := NormalizeAccountReference(models.AccountReference{Namespace: tc.namespace, Kind: tc.kind, Value: tc.value})
		require.NoError(t, err)
		require.Equal(t, tc.want, ref.Value)
	}
	for _, ref := range []models.AccountReference{
		{Namespace: "reddit", Kind: "id", Value: "one"},
		{Namespace: "mirror:coomer", Kind: "user", Value: "one"},
		{Namespace: "native:instagram", Kind: "id", Value: ""},
		{Namespace: "native:instagram", Kind: "id", Value: " padded "},
		{Namespace: "native:instagram", Kind: "id", Value: "with\x00control"},
		{Namespace: "url", Kind: "profile", Value: "file:///private"},
		{Namespace: "url", Kind: "id", Value: "https://example.test/account"},
	} {
		_, err := NormalizeAccountReference(ref)
		require.Error(t, err, "%+v", ref)
	}
}
