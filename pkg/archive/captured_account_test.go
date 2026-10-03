package archive

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCapturedAccountsFromSupportedExtractors(t *testing.T) {
	for _, tc := range []struct {
		name, raw, namespace, kind, value, handle, path string
	}{
		{"reddit", `{"category":"reddit","author":"ExampleAuthor","author_fullname":"t2_ab12","user":{"id":"wrong","name":"feed-owner"}}`, "native:reddit", "id", "t2_ab12", "exampleauthor", "/author_fullname"},
		{"reddit parent", `{"category":"redgifs","user":{"id":"child-owner"},"_reddit":{"id":"post","author_fullname":"t2_ab12","author":"ExampleAuthor"}}`, "native:reddit", "id", "t2_ab12", "exampleauthor", "/_reddit/author_fullname"},
		{"twitter", `{"category":"twitter","author":{"id":9007199254740993,"name":"ExampleAuthor","nick":"A display name"},"user":{"id":"feed-owner"}}`, "native:twitter", "id", "9007199254740993", "exampleauthor", "/author/id"},
		{"instagram", `{"category":"instagram","owner_id":"100","username":"Example.Author","post_id":"not-an-account"}`, "native:instagram", "id", "100", "example.author", "/owner_id"},
		{"bluesky", `{"category":"bluesky","author":{"did":"did:plc:Example","handle":"Example.test"}}`, "native:bluesky", "id", "did:plc:Example", "example.test", "/author/did"},
		{"tiktok numeric", `{"category":"tiktok","author":{"id":"100","uniqueId":"ExampleAuthor"}}`, "native:tiktok", "id", "100", "exampleauthor", "/author/id"},
		{"tiktok secondary", `{"category":"tiktok","author":{"secUid":"OpaqueSecondary","uniqueId":"ExampleAuthor"}}`, "native:tiktok", "secUid", "OpaqueSecondary", "exampleauthor", "/author/secUid"},
		{"tumblr", `{"category":"tumblr","blog":{"uuid":"OpaqueBlog","name":"ExampleAuthor"}}`, "native:tumblr", "id", "OpaqueBlog", "exampleauthor", "/blog/uuid"},
		{"onlyfans", `{"category":"onlyfans","user":{"id":"100","username":"ExampleAuthor"}}`, "native:onlyfans", "id", "100", "exampleauthor", "/user/id"},
		{"fansly", `{"category":"fansly","user":{"id":"100","username":"ExampleAuthor"}}`, "native:fansly", "id", "100", "exampleauthor", "/user/id"},
		{"fansly current", `{"category":"fansly","account":{"id":"100","username":"ExampleAuthor"},"user":{"id":"other"}}`, "native:fansly", "id", "100", "exampleauthor", "/account/id"},
		{"patreon", `{"category":"patreon","creator":{"id":"100","username":"ExampleAuthor"}}`, "native:patreon", "id", "100", "exampleauthor", "/creator/id"},
		{"pixiv", `{"category":"pixiv","user":{"id":1234,"account":"CaseSensitive","name":"Display label"}}`, "native:pixiv", "id", "1234", "CaseSensitive", "/user/id"},
		{"unknown nested", `{"category":"new-service","owner":{"uuid":"Opaque-ID","handle":"CaseSensitive"}}`, "native:new-service", "id", "Opaque-ID", "CaseSensitive", "/owner/uuid"},
		{"unknown scalar", `{"category":"new-service","user_id":"Opaque-ID","username":"CaseSensitive"}`, "native:new-service", "id", "Opaque-ID", "CaseSensitive", "/user_id"},
		{"coomer", `{"category":"coomer","service":"onlyfans","user":"CaseSensitive","username":"Display label"}`, "mirror:coomer:onlyfans", "user", "CaseSensitive", "", "/user"},
		{"kemono", `{"category":"kemono","service":"patreon","user":"100","username":"Display label"}`, "mirror:kemono:patreon", "user", "100", "", "/user"},
		{"kemono unfamiliar service", `{"category":"kemono","service":"new-service","user":"100","username":"Display label"}`, "mirror:kemono:new-service", "user", "100", "", "/user"},
		{"yt-dlp", `{"category":"ytdl","extractor_key":"ExampleVideo","channel_id":"OpaqueChannel","uploader":"Display label"}`, "ytdl:examplevideo", "id", "OpaqueChannel", "", "/channel_id"},
		{"yt-dlp generic", `{"category":"ytdl-generic","extractor_key":"Generic","webpage_url":"https://video.example.test/watch/123","uploader_id":"OpaqueUploader"}`, "ytdl:video.example.test", "id", "OpaqueUploader", "", "/uploader_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(tc.raw)
			before := bytes.Clone(raw)
			account, err := ExtractCapturedAccount(raw)
			require.NoError(t, err)
			require.NotNil(t, account)
			require.Equal(t, CapturedAccountPolicy, account.Policy)
			require.Equal(t, before, raw, "extractor metadata must remain intact")
			require.Equal(t, tc.namespace, account.Namespace)
			require.Equal(t, models.AccountReference{Namespace: tc.namespace, Kind: tc.kind, Value: tc.value}, account.Identifiers[0].Reference)
			require.Equal(t, tc.path, account.Identifiers[0].Path)
			if tc.handle != "" {
				require.Len(t, account.Identifiers, 2)
				require.Equal(t, models.AccountReference{Namespace: tc.namespace, Kind: "handle", Value: tc.handle}, account.Identifiers[1].Reference)
			} else {
				require.Len(t, account.Identifiers, 1)
			}
			replay, err := ExtractCapturedAccount(raw)
			require.NoError(t, err)
			require.Equal(t, account, replay)
		})
	}
}

func TestCapturedAccountsDoNotBorrowOrInventIdentity(t *testing.T) {
	for _, raw := range []string{
		`{"category":"instagram","post_id":"post","source_extractor_url":"https://instagram.com/example","directory":"Example, instagram"}`,
		`{"category":"reddit","id":"post","author":"ExampleAuthor","user":{"id":"feed-owner","name":"SomeoneElse"}}`,
		`{"category":"twitter","author":{"name":"ExampleAuthor"},"user":{"id":"feed-owner","name":"SomeoneElse"}}`,
		`{"category":"reddit","title":"ExampleAuthor","user":{"id":"feed-owner"}}`,
		`{"category":"new-service","author":"ExampleAuthor","user":{"id":"feed-owner","username":"SomeoneElse"}}`,
		`{"category":"new-service","author":{"name":"ExampleAuthor"},"user":{"id":"feed-owner"}}`,
		`{"user":{"id":"100","username":"ExampleAuthor"}}`,
		`{"category":"coomer","user":"100"}`,
		`{"category":"ytdl","extractor_key":"Generic","uploader_id":"100"}`,
		`{"category":"ytdl","extractor_key":"Generic","webpage_url":"file:///local","uploader_id":"100"}`,
		`{"category":"leakgallery","creator":"ExampleAuthor","id":"post"}`,
		`{"category":"fansly","account":{"username":"ExampleAuthor"},"user":{"id":"other"}}`,
	} {
		account, err := ExtractCapturedAccount([]byte(raw))
		require.NoError(t, err, raw)
		require.Nil(t, account, raw)
	}
	// An unfamiliar extractor's display name stays available but is not
	// promoted to a handle. Separate services retain separate identifier scopes.
	account, err := ExtractCapturedAccount([]byte(`{"category":"new-service","user":{"id":"100","name":"Display Name"}}`))
	require.NoError(t, err)
	require.Equal(t, "Display Name", account.Label)
	require.Len(t, account.Identifiers, 1)
}

func TestCapturedAccountsRetainSecondaryAndMirrorPublicIdentifiers(t *testing.T) {
	account, err := ExtractCapturedAccount([]byte(`{"category":"tiktok","author":{"id":"100","secUid":"OpaqueSecondary","uniqueId":"ExampleAuthor"}}`))
	require.NoError(t, err)
	require.Equal(t, []CapturedAccountIdentifier{
		{Reference: models.AccountReference{Namespace: "native:tiktok", Kind: "id", Value: "100"}, Basis: "captured-author", Path: "/author/id"},
		{Reference: models.AccountReference{Namespace: "native:tiktok", Kind: "secUid", Value: "OpaqueSecondary"}, Basis: "captured-author", Path: "/author/secUid"},
		{Reference: models.AccountReference{Namespace: "native:tiktok", Kind: "handle", Value: "exampleauthor"}, Basis: "captured-author", Path: "/author/uniqueId"},
	}, account.Identifiers)
	for _, id := range []string{"100", "another-account"} {
		raw, err := json.Marshal(map[string]interface{}{"category": "kemono", "service": "patreon", "user": "100", "username": "Display Name",
			"user_profile": map[string]interface{}{"id": id, "service": "patreon", "public_id": "PublicHandle"}})
		require.NoError(t, err)
		account, err := ExtractCapturedAccount(raw)
		require.NoError(t, err)
		require.Equal(t, "Display Name", account.Label)
		if id == "100" {
			require.Len(t, account.Identifiers, 2)
			require.Equal(t, CapturedAccountIdentifier{Reference: models.AccountReference{Namespace: "mirror:kemono:patreon", Kind: "public_id", Value: "PublicHandle"},
				Basis: "captured-mirror-public-id", Path: "/user_profile/public_id"}, account.Identifiers[1])
		} else {
			require.Len(t, account.Identifiers, 1)
		}
	}
	account, err = ExtractCapturedAccount([]byte(`{"category":"kemono","service":"patreon","user":"100","user_profile":{"id":"100","service":"fanbox","public_id":"WrongService"}}`))
	require.NoError(t, err)
	require.Len(t, account.Identifiers, 1)
}

func TestCapturedAccountNoiseAndRetainedPayloadHaveSameIdentity(t *testing.T) {
	raw := []byte(`{"category":"reddit","author":"ExampleAuthor","author_fullname":"t2_ab12","score":123,"user":{"id":"ab12","name":"ExampleAuthor","default_set":true,"subscribers":10}}`)
	before, err := ExtractCapturedAccount(raw)
	require.NoError(t, err)
	retained, err := RetainSourcePayload(raw)
	require.NoError(t, err)
	after, err := ExtractCapturedAccount(retained)
	require.NoError(t, err)
	require.Equal(t, before, after)
	for _, raw := range []string{
		`{"category":"reddit","author":"ExampleAuthor","author_fullname":"t2_ab12","score":999,"user":{"id":"ab12","name":"ExampleAuthor","default_set":false}}`,
		`{"category":"reddit","author":"ExampleAuthor","author_fullname":"t2_ab12","num":2,"filename":"another-media"}`,
	} {
		after, err := ExtractCapturedAccount([]byte(raw))
		require.NoError(t, err)
		require.Equal(t, before, after)
	}
}

func TestCapturedAccountInvalidClaimsAreReported(t *testing.T) {
	for _, raw := range []string{
		`{"category":"instagram","owner_id":true}`,
		`{"category":"instagram","owner_id":1.5}`,
		`{"category":"instagram","owner_id":1e20}`,
		`{"category":"instagram","owner_id":{"id":"100"}}`,
		`{"category":"instagram","owner_id":["100"]}`,
		`{"category":"fansly","account":[],"user":{"id":"other"}}`,
		`{"category":"instagram","owner_id":"100","username":{"name":"nested"}}`,
		`{"category":"instagram","owner_id":" padded "}`,
		`{"category":"instagram","owner_id":"with\u0000control"}`,
		`{"category":"service/bad","user":{"id":"100"}}`,
		`{"category":"instagram","owner_id":"first","owner_id":"second"}`,
		`{"category":"instagram","owner_id":"\ud800"}`,
	} {
		account, err := ExtractCapturedAccount([]byte(raw))
		require.Error(t, err, raw)
		require.Nil(t, account, raw)
	}
	raw, err := json.Marshal(map[string]interface{}{"category": "instagram", "owner_id": strings.Repeat("x", 2049)})
	require.NoError(t, err)
	_, err = ExtractCapturedAccount(raw)
	require.Error(t, err)
	_, err = ExtractCapturedAccount(bytes.Repeat([]byte{' '}, MaxSourcePayloadBytes+1))
	require.Error(t, err)
}
