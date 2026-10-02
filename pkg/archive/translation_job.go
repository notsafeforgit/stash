package archive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

const MaxTranslationJobTargets = 50

func translationUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func translationDigest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func TranslationResource(request string) string {
	return translationDigest([]byte("translation-cache-request\x00" + request))
}

// Targets must be sorted by UUID, with no repeated target, even at another
// revision. Priority is supplied separately from the frozen work identity.
func PrepareTranslationJob(input models.TranslationJobArguments) (models.ArchiveJobSubmission, error) {
	ret := models.ArchiveJobSubmission{}
	if input.Version != 1 || !translationUUID(input.RequestUUID) || len(input.Targets) == 0 || len(input.Targets) > MaxTranslationJobTargets {
		return ret, models.ErrTranslationWorkInvalid
	}
	previous := ""
	for _, target := range input.Targets {
		if !translationUUID(target.TargetUUID) || target.TargetUUID <= previous || target.Revision < 1 {
			return ret, models.ErrTranslationWorkInvalid
		}
		previous = target.TargetUUID
	}
	body, err := json.Marshal(input)
	if err != nil {
		return ret, err
	}
	tree, err := DecodeJSONObject(body, 16384)
	if err != nil {
		return ret, err
	}
	body, err = EncodeSourceJSON(tree)
	if err != nil {
		return ret, err
	}
	key := translationDigest(body)
	return models.ArchiveJobSubmission{RequestUUID: uuid.NewSHA1(uuid.NameSpaceURL, []byte("urn:stash:translation-job:v1:"+key)).String(),
		Kind: models.ArchiveJobTranslateText, WorkKey: key, ResourceKey: TranslationResource(input.RequestUUID), Arguments: body, MaxAttempts: 10}, nil
}

func DecodeTranslationJob(current *models.ArchiveJob) (*models.TranslationJobArguments, error) {
	if current == nil || current.Kind != models.ArchiveJobTranslateText {
		return nil, models.ErrTranslationWorkInvalid
	}
	if _, err := DecodeJSONObject(current.Arguments, 16384); err != nil {
		return nil, models.ErrTranslationWorkInvalid
	}
	var input models.TranslationJobArguments
	d := json.NewDecoder(bytes.NewReader(current.Arguments))
	d.DisallowUnknownFields()
	if err := d.Decode(&input); err != nil {
		return nil, models.ErrTranslationWorkInvalid
	}
	expected, err := PrepareTranslationJob(input)
	if err != nil || expected.WorkKey != current.WorkKey || expected.ResourceKey != current.ResourceKey {
		return nil, models.ErrTranslationWorkInvalid
	}
	return &input, nil
}
