package archive

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollateLegacyNFOFieldsPreservesDomainValues(t *testing.T) {
	input := json.RawMessage(`{"title":["  A title\n"],"plot":["Description <&> 🌿"],"premiered":["2026-07-05"],"url":["https://example.invalid/post/1","https://example.invalid/post/1","https://example.invalid/post/2"],"actor":["Performer","Alias"],"studio":["Studio"]}`)
	before := append([]byte(nil), input...)
	result, err := CollateLegacyNFOFields(input)
	require.NoError(t, err)
	require.Equal(t, "  A title\n", *result.Metadata.Title)
	require.Equal(t, "Description <&> 🌿", *result.Metadata.OriginalText)
	require.Equal(t, "2026-07-05", *result.Metadata.PublishedAt)
	require.Equal(t, "legacy-nfo-unverified", *result.Metadata.DateBasis)
	require.Nil(t, result.Metadata.Language)
	require.Equal(t, []string{"https://example.invalid/post/1", "https://example.invalid/post/2"}, result.URLs)
	require.Equal(t, []string{"Performer", "Alias"}, result.Performers)
	require.Equal(t, "Studio", *result.Studio)
	require.Empty(t, result.Translations)
	require.Equal(t, before, []byte(input))
}

func TestCollateLegacyNFOFieldsKeepsOriginalsAndSharesTranslations(t *testing.T) {
	result, err := CollateLegacyNFOFields(json.RawMessage(`{"title":["Translated"],"original-title":["  原文\n"],"plot":["Translated"],"original-plot":["  原文\n"],"translation-language":["en"],"translation-provider":["saved-provider"]}`))
	require.NoError(t, err)
	require.Equal(t, "  原文\n", *result.Metadata.Title)
	require.Equal(t, "  原文\n", *result.Metadata.OriginalText)
	require.Nil(t, result.Metadata.Language, "the translation language is not the original's language")
	require.Len(t, result.Translations, 1, "title and caption can share one translation result")
	translation := result.Translations[0]
	require.Equal(t, "  原文\n", *translation.OriginalText)
	require.Equal(t, "Translated", translation.TranslatedText)
	require.Equal(t, "en", *translation.TargetLanguage)
	require.Equal(t, "saved-provider", *translation.Provider)
	require.Nil(t, translation.SourceLanguage)

	result, err = CollateLegacyNFOFields(json.RawMessage(`{"plot":["Translation"],"original-plot":["Original"]}`))
	require.NoError(t, err)
	require.Len(t, result.Translations, 1)
	require.Nil(t, result.Translations[0].TargetLanguage, "missing language is not assumed to be English")
	require.Nil(t, result.Translations[0].Provider)

	result, err = CollateLegacyNFOFields(json.RawMessage(`{"plot":["Unchanged"],"original-plot":["Unchanged"]}`))
	require.NoError(t, err)
	require.Equal(t, "Unchanged", *result.Metadata.OriginalText)
	require.Empty(t, result.Translations)
}

func TestCollateLegacyNFOFieldsPreservesMissingAndEmptyValues(t *testing.T) {
	result, err := CollateLegacyNFOFields(json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Nil(t, result.Metadata.Title)
	require.Nil(t, result.Metadata.PublishedAt)
	require.Nil(t, result.Metadata.DateBasis)

	result, err = CollateLegacyNFOFields(json.RawMessage(`{"title":["",""],"plot":[],"original-plot":[""]}`))
	require.NoError(t, err)
	require.NotNil(t, result.Metadata.Title)
	require.Empty(t, *result.Metadata.Title)
	require.NotNil(t, result.Metadata.OriginalText)
	require.Empty(t, *result.Metadata.OriginalText)
	require.Empty(t, result.Translations)
}

func TestCollateLegacyNFOFieldsRejectsUnaccountedValues(t *testing.T) {
	for _, input := range []string{
		`{"unmapped":["keep me"]}`,
		`{"nfo_path":["do not retain.nfo"]}`,
		`{"title":["one","two"]}`,
		`{"studio":["one","two"]}`,
		`{"title":"string instead of array"}`,
		`{"title":[123]}`,
		`{"actor":[{"name":"Performer"}]}`,
		`{"translation-language":["en"]}`,
		`{"plot":["Translation"],"translation-provider":["unknown original"]}`,
		`{"title":null}`,
		`[]`,
		`null`,
		`{"title":["` + strings.Repeat("x", MaxSourcePayloadBytes) + `"]}`,
	} {
		_, err := CollateLegacyNFOFields(json.RawMessage(input))
		require.Error(t, err)
	}
}
