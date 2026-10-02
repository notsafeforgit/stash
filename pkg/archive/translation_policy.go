package archive

import (
	"bytes"
	"encoding/json"

	"github.com/stashapp/stash/pkg/models"
)

func ValidateTranslationPolicy(input models.TranslationPolicyDefinition) error {
	if input.Priority < 0 || input.Priority > 100 || (input.Enabled && !input.Title && !input.Caption) {
		return models.ErrTranslationPolicyInvalid
	}
	if _, err := PrepareTranslationRequest(models.TranslationRequestInput{Policy: input.ProviderPolicy, TargetLanguage: input.TargetLanguage}); err != nil {
		return models.ErrTranslationPolicyInvalid
	}
	return nil
}

func DecodeTranslationPolicy(body []byte) (models.TranslationPolicyDefinition, error) {
	var ret models.TranslationPolicyDefinition
	if _, err := DecodeJSONObject(body, 1024); err != nil {
		return ret, models.ErrTranslationPolicyInvalid
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(&ret); err != nil {
		return ret, models.ErrTranslationPolicyInvalid
	}
	return ret, ValidateTranslationPolicy(ret)
}
