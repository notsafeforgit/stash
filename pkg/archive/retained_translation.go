package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

const MaxTranslationTextBytes = 4 << 20

func translationString(value *string, limit int) bool {
	return value == nil || (len(*value) <= limit && utf8.ValidString(*value))
}

func PrepareSourceTranslation(input models.SourceTranslationInput) (*models.SourceTranslation, error) {
	if !translationString(input.OriginalText, MaxTranslationTextBytes) || !translationString(&input.TranslatedText, MaxTranslationTextBytes) ||
		!translationString(input.SourceLanguage, 128) || !translationString(input.TargetLanguage, 128) || !translationString(input.Provider, 1024) {
		return nil, models.ErrSourceTranslationInvalid
	}
	ret := &models.SourceTranslation{SourceTranslationInput: input}
	if input.OriginalText != nil {
		sum := sha256.Sum256([]byte(*input.OriginalText))
		hash := hex.EncodeToString(sum[:])
		ret.OriginalSHA256 = &hash
	}
	body, err := json.Marshal([]any{input.OriginalText, input.TranslatedText, input.SourceLanguage, input.TargetLanguage, input.Provider})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	ret.UUID = uuid.NewSHA1(uuid.NameSpaceURL, []byte("urn:stash:source-translation:v1:"+hex.EncodeToString(sum[:]))).String()
	return ret, nil
}
