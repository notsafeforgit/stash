package archive

import (
	"encoding/json"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

// LegacyNFOPostData is a transient projection used when removing imported NFO
// documents. It contains domain values only, not document IDs, paths, XML,
// checksums, or retirement records. Callers must reconcile these values with the
// existing post, translations, and defaults before discarding the input.
type LegacyNFOPostData struct {
	Metadata     models.SourcePostMetadata
	URLs         []string
	Performers   []string
	Studio       *string
	Translations []models.SourceTranslationInput
}

// CollateLegacyNFOFields reads the already-parsed fields in a completed catalog
// import. It neither opens sidecars nor imports the catalog again. Unknown or
// conflicting scalar values are errors so cleanup cannot silently discard them.
func CollateLegacyNFOFields(parsed json.RawMessage) (*LegacyNFOPostData, error) {
	object, err := DecodeJSONObject(parsed, MaxSourcePayloadBytes)
	if err != nil {
		return nil, err
	}
	known := sourceKeys("title original-title plot original-plot premiered url actor studio translation-language translation-provider")
	fields := make(map[string][]string, len(object))
	for key, raw := range object {
		if !known[key] {
			return nil, fmt.Errorf("unmapped NFO field %q", key)
		}
		values, ok := raw.([]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid NFO field %q: expected a string array", key)
		}
		seen := make(map[string]bool, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("invalid NFO field %q: expected a string", key)
			}
			if !seen[text] {
				fields[key] = append(fields[key], text)
				seen[text] = true
			}
		}
		if key != "url" && key != "actor" && len(fields[key]) > 1 {
			return nil, fmt.Errorf("conflicting NFO values for %q", key)
		}
	}
	value := func(key string) *string {
		if values := fields[key]; len(values) != 0 {
			return &values[0]
		}
		return nil
	}
	ret := &LegacyNFOPostData{
		URLs: fields["url"], Performers: fields["actor"], Studio: value("studio"),
		Metadata: models.SourcePostMetadata{PublishedAt: value("premiered")},
	}
	if ret.Metadata.PublishedAt != nil {
		basis := "legacy-nfo-unverified"
		ret.Metadata.DateBasis = &basis
	}

	// The translated plot/title must not replace the original text. Native
	// translation results already share identical pairs between posts.
	seenTranslations := make(map[string]bool)
	for _, field := range []struct {
		original, display string
		out               **string
	}{
		{"original-title", "title", &ret.Metadata.Title},
		{"original-plot", "plot", &ret.Metadata.OriginalText},
	} {
		original, display := value(field.original), value(field.display)
		*field.out = display
		if original == nil {
			continue
		}
		*field.out = original
		if display == nil {
			continue
		}
		language, provider := value("translation-language"), value("translation-provider")
		if *display == *original && language == nil && provider == nil {
			continue
		}
		input := models.SourceTranslationInput{OriginalText: original, TranslatedText: *display,
			TargetLanguage: language, Provider: provider}
		translation, err := PrepareSourceTranslation(input)
		if err != nil {
			return nil, err
		}
		if !seenTranslations[translation.UUID] {
			ret.Translations = append(ret.Translations, input)
			seenTranslations[translation.UUID] = true
		}
	}
	if len(ret.Translations) == 0 && (value("translation-language") != nil || value("translation-provider") != nil) {
		return nil, fmt.Errorf("NFO translation metadata has no original/display text pair")
	}
	return ret, nil
}
