package metadata

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMetadataPolicyImportPreparationRejectsUnaccountedAndInvalidEvidence(t *testing.T) {
	input := models.MetadataPolicyImportInput{UUID: uuid.NewString(), Policy: models.MetadataPolicyInput{
		CollectionUUID: uuid.NewString(), ExpectedCollectionRevision: 1, Origin: "migration", Reason: "Reviewed conversion",
		Definition: models.MetadataPolicyDefinition{Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{}}},
		Document: models.MetadataPolicyImportDocument{Format: "legacy-metadata-policy", Version: 1, PluginVersion: "1.14.1", CapturedAt: "2026-09-29T00:00:00Z",
			SourceFiles: map[string]string{"config.py": strings.Repeat("a", 64)}, Values: map[string]json.RawMessage{"python/title": json.RawMessage(`"Original title"`)}},
		Dispositions:  map[string]models.MetadataPolicyImportDisposition{"python/title": {Action: "mapped", Reason: "Native title mapping"}},
		FolderSources: []models.MetadataPolicyImportDocumentRef{}}
	body, digest, review, err := PreparePolicyImport(input, time.Now())
	require.NoError(t, err)
	require.Empty(t, review)
	require.Len(t, digest, 64)
	var restored models.MetadataPolicyImportInput
	require.NoError(t, json.Unmarshal(body, &restored))
	again, againHash, _, err := PreparePolicyImport(restored, time.Now())
	require.NoError(t, err)
	require.Equal(t, body, again)
	require.Equal(t, digest, againHash)
	for _, raw := range []string{`{"duplicate":1,"duplicate":2}`, `"\ud800"`, strings.Repeat("[", 66) + `0` + strings.Repeat("]", 66)} {
		input.Document.Values["python/title"] = json.RawMessage(raw)
		_, _, rejectedReview, err := PreparePolicyImport(input, time.Now())
		require.ErrorIs(t, err, models.ErrMetadataPolicyImportInvalid)
		require.Nil(t, rejectedReview)
	}
	input.Document.Values["python/title"] = json.RawMessage(`null`)
	delete(input.Dispositions, "python/title")
	_, _, review, err = PreparePolicyImport(input, time.Now())
	require.ErrorIs(t, err, models.ErrMetadataPolicyImportInvalid)
	require.Nil(t, review)
}
