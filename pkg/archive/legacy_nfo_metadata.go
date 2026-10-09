package archive

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/models"
)

var generatedNFOEntity = regexp.MustCompile(`&(?:amp|lt|gt|quot|apos|#[0-9]+|#x[0-9a-fA-F]+);`)

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
	// A UUID selects a shared translation; null explicitly keeps the original.
	// These references preserve the saved display choice without copying text.
	DisplayTranslations map[string]*string `json:",omitempty"`
}

// RecoverGeneratedNFOFields handles the old writer's unescaped text bug. The
// wrapper and field order must match that writer exactly; this is not a lenient
// XML parser and never resolves external entities or guesses missing fields.
func RecoverGeneratedNFOFields(raw []byte) (json.RawMessage, error) {
	if len(raw) > MaxDocumentBytes || !utf8.Valid(raw) {
		return nil, fmt.Errorf("invalid generated NFO text")
	}
	text := strings.TrimSpace(string(raw))
	if strings.HasPrefix(text, "<?xml") {
		end := strings.Index(text, "?>")
		if end < 0 {
			return nil, fmt.Errorf("invalid NFO declaration")
		}
		text = strings.TrimSpace(text[end+2:])
	}
	if !strings.HasPrefix(text, "<movie>") {
		return nil, fmt.Errorf("unrecognized generated NFO wrapper")
	}
	text = strings.TrimSpace(strings.TrimPrefix(text, "<movie>"))
	fields := map[string][]string{}
	for _, key := range []string{"url", "premiered", "title", "plot"} {
		opening, closing := "<"+key+">", "</"+key+">"
		if !strings.HasPrefix(text, opening) {
			return nil, fmt.Errorf("unrecognized generated NFO field order")
		}
		text = strings.TrimPrefix(text, opening)
		end := strings.Index(text, closing)
		if end < 0 {
			return nil, fmt.Errorf("incomplete generated NFO field")
		}
		value := text[:end]
		// Text such as "<!" and "<?" can be literal emoticons from the old
		// writer. Field contents are plain text here, never parsed as markup.
		if strings.ContainsRune(value, 0) || strings.Contains(value, opening) {
			return nil, fmt.Errorf("unsupported generated NFO text")
		}
		// Decode complete XML character references once. HTML's permissive
		// partial-entity rules would change literal text such as "&notable".
		fields[key] = []string{generatedNFOEntity.ReplaceAllStringFunc(value, html.UnescapeString)}
		text = strings.TrimSpace(text[end+len(closing):])
	}
	if text != "</movie>" {
		return nil, fmt.Errorf("unexpected generated NFO contents")
	}
	return json.Marshal(fields)
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
		original, display, metadataField string
		out                              **string
	}{
		{"original-title", "title", "title", &ret.Metadata.Title},
		{"original-plot", "plot", "original_text", &ret.Metadata.OriginalText},
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
		if *original != "" && *display != "" {
			if ret.DisplayTranslations == nil {
				ret.DisplayTranslations = map[string]*string{}
			}
			ret.DisplayTranslations[field.metadataField] = nil
		}
		if *display == *original && language == nil && provider == nil {
			continue
		}
		input := models.SourceTranslationInput{OriginalText: original, TranslatedText: *display,
			TargetLanguage: language, Provider: provider}
		translation, err := PrepareSourceTranslation(input)
		if err != nil {
			return nil, err
		}
		if *original != "" && *display != "" {
			ret.DisplayTranslations[field.metadataField] = &translation.UUID
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
