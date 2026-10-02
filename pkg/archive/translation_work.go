package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

func translationIdentity(kind string, value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", models.ErrTranslationWorkInvalid
	}
	sum := sha256.Sum256(body)
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("urn:stash:"+kind+":v1:"+hex.EncodeToString(sum[:]))).String(), nil
}

func PrepareTranslationRequest(input models.TranslationRequestInput) (*models.TranslationRequest, error) {
	if !utf8.ValidString(input.OriginalText) || len(input.OriginalText) > MaxTranslationTextBytes ||
		input.Policy != models.TranslationBingTextV1 || !validTranslationLanguage(input.TargetLanguage) {
		return nil, models.ErrTranslationWorkInvalid
	}
	sum := sha256.Sum256([]byte(input.OriginalText))
	id, err := translationIdentity("translation-request", []string{input.Policy, input.OriginalText, input.TargetLanguage})
	if err != nil {
		return nil, err
	}
	return &models.TranslationRequest{TranslationRequestInput: input, OriginalSHA256: hex.EncodeToString(sum[:]), UUID: id}, nil
}

func validTranslationLanguage(value string) bool {
	if value == "" || len(value) > 128 || strings.Trim(value, "-") != value {
		return false
	}
	for _, c := range value {
		letter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		digit := c >= '0' && c <= '9'
		if !letter && !digit && c != '-' {
			return false
		}
	}
	return !strings.Contains(value, "--")
}

func translationLanguageBase(value string) string {
	return strings.Split(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "_", "-"), "-")[0]
}

func PrepareTranslationCache(request *models.TranslationRequest, input models.TranslationCacheInput) (*models.TranslationCache, *models.SourceTranslation, error) {
	if request == nil || input.RequestUUID != request.UUID || !ValidDocumentSourceTime(input.CapturedAt) ||
		(input.Origin != "worker" && input.Origin != "migration" && input.Origin != "review") {
		return nil, nil, models.ErrTranslationWorkInvalid
	}
	var result *models.SourceTranslation
	var resultUUID *string
	switch input.Status {
	case "translated", "unchanged":
		if input.TranslatedText == nil || strings.TrimSpace(*input.TranslatedText) == "" {
			return nil, nil, models.ErrTranslationWorkInvalid
		}
		if input.Status == "unchanged" && (*input.TranslatedText != request.OriginalText || input.SourceLanguage == nil ||
			translationLanguageBase(*input.SourceLanguage) != translationLanguageBase(request.TargetLanguage)) {
			return nil, nil, models.ErrTranslationWorkInvalid
		}
		var err error
		result, err = PrepareSourceTranslation(models.SourceTranslationInput{OriginalText: &request.OriginalText, TranslatedText: *input.TranslatedText,
			SourceLanguage: input.SourceLanguage, TargetLanguage: &request.TargetLanguage, Provider: input.Provider})
		if err != nil {
			return nil, nil, models.ErrTranslationWorkInvalid
		}
		resultUUID = &result.UUID
	case "no_text":
		if input.TranslatedText != nil || input.SourceLanguage != nil || input.Provider != nil {
			return nil, nil, models.ErrTranslationWorkInvalid
		}
	default:
		return nil, nil, models.ErrTranslationWorkInvalid
	}
	ret := &models.TranslationCache{RequestUUID: request.UUID, Status: input.Status, TranslationUUID: resultUUID, CapturedAt: input.CapturedAt, Origin: input.Origin}
	id, err := translationIdentity("translation-cache", []any{ret.RequestUUID, ret.Status, ret.TranslationUUID})
	if err != nil {
		return nil, nil, err
	}
	ret.UUID = id
	return ret, result, nil
}

func TranslationTargetIdentity(input models.TranslationTargetInput) (string, error) {
	validID := func(value string) bool {
		parsed, err := uuid.Parse(value)
		return err == nil && parsed != uuid.Nil && parsed.String() == value
	}
	if !validID(input.RequestUUID) || !validID(input.PostUUID) || (input.CollectionUUID == nil) != (input.CollectionRevision == nil) ||
		(input.CollectionUUID != nil && (!validID(*input.CollectionUUID) || *input.CollectionRevision < 1)) ||
		(input.Field != "title" && input.Field != "caption") || (input.Origin != "capture" && input.Origin != "migration" && input.Origin != "review") {
		return "", models.ErrTranslationWorkInvalid
	}
	return translationIdentity("translation-target", []any{input.RequestUUID, input.PostUUID, input.CollectionUUID, input.CollectionRevision, input.Field})
}
