package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

type policyTranslationReader struct {
	models.SourceTranslationReaderWriter
	t          *testing.T
	rows       map[string][]models.SourceTranslationEvidence
	results    map[string]*models.SourceTranslation
	queries    int
	finds      map[string]int
	queryError error
	findError  error
}

func (r *policyTranslationReader) PostEvidence(_ context.Context, query models.SourceTranslationQuery) ([]models.SourceTranslationEvidence, error) {
	require.Equal(r.t, "selected-post", query.PostUUID)
	require.Nil(r.t, query.TargetLanguage, "language is selected by the mapping, never implicitly English")
	require.Equal(r.t, 100, query.Limit)
	r.queries++
	rows, start := r.rows[query.OriginalSHA256], 0
	for start < len(rows) && rows[start].UUID <= query.After {
		start++
	}
	return rows[start:min(start+query.Limit, len(rows))], r.queryError
}

func (r *policyTranslationReader) Find(_ context.Context, id string) (*models.SourceTranslation, error) {
	r.finds[id]++
	return r.results[id], r.findError
}

func newPolicyTranslationReader(t *testing.T, originals []string, results, repetitions, textBytes int) *policyTranslationReader {
	t.Helper()
	r := &policyTranslationReader{t: t, rows: make(map[string][]models.SourceTranslationEvidence),
		results: make(map[string]*models.SourceTranslation), finds: make(map[string]int)}
	for _, original := range originals {
		sum := sha256.Sum256([]byte(original))
		hash := hex.EncodeToString(sum[:])
		for resultIndex := range results {
			result, err := archive.PrepareSourceTranslation(models.SourceTranslationInput{
				OriginalText: &original, TranslatedText: fmt.Sprintf("%03d-", resultIndex) + strings.Repeat("t", textBytes),
			})
			require.NoError(t, err)
			r.results[result.UUID] = result
			for range repetitions {
				index := len(r.rows[hash])
				r.rows[hash] = append(r.rows[hash], models.SourceTranslationEvidence{UUID: fmt.Sprintf("evidence-%08d", index),
					PostUUID: "selected-post", TranslationUUID: result.UUID, CapturedAt: "2026-10-01T00:00:00Z"})
			}
		}
	}
	return r
}

func TestPolicyTranslationsShareResultsAndBoundCompleteSelections(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		originals                  []string
		results, repeats, textSize int
		complete                   bool
	}{
		{"empty", []string{"original"}, 0, 0, 0, true},
		{"repeated_pages", []string{"original"}, 1, 207, 20, true},
		{"two_originals", []string{"title", "caption"}, 3, 2, 20, true},
		{"distinct_limit", []string{"original"}, 128, 1, 20, true},
		{"distinct_overflow", []string{"original"}, 129, 1, 20, false},
		{"evidence_limit", []string{"original"}, 1, 4096, 20, true},
		{"evidence_overflow", []string{"original"}, 1, 4097, 20, false},
		{"byte_overflow", []string{"original"}, 1, 1, 1 << 20, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := newPolicyTranslationReader(t, tc.originals, tc.results, tc.repeats, tc.textSize)
			capture := &models.SourceCapture{PostUUID: "selected-post", Metadata: models.SourcePostMetadata{
				Title: &tc.originals[0], OriginalText: &tc.originals[len(tc.originals)-1],
			}}
			values, complete, err := (Service{Repo: models.Repository{SourceTranslation: reader}}).sourcePostTranslations(t.Context(), capture)
			require.NoError(t, err)
			require.Equal(t, tc.complete, complete)
			require.LessOrEqual(t, reader.queries, 42)
			for _, count := range reader.finds {
				require.Equal(t, 1, count, "repeated assertions must not reload a shared body")
			}
			if !complete {
				require.Nil(t, values, "an incomplete set must never look like all candidate translations")
				return
			}
			require.NotNil(t, values)
			require.Len(t, values, len(tc.originals)*tc.results)
			for index, value := range values {
				if len(tc.originals) == 1 {
					require.Equal(t, []string{"title", "original_text"}, value.SourceFields)
				} else {
					require.Len(t, value.SourceFields, 1)
				}
				if index > 0 {
					require.Less(t, values[index-1].UUID, value.UUID)
				}
			}
		})
	}
}

func TestPolicyTranslationMissingOriginalsAndReadErrors(t *testing.T) {
	empty := ""
	for _, original := range []*string{nil, &empty} {
		values, complete, err := (Service{}).sourcePostTranslations(t.Context(), &models.SourceCapture{Metadata: models.SourcePostMetadata{Title: original}})
		require.NoError(t, err)
		require.True(t, complete)
		require.Empty(t, values, "no invented original means no translation lookup")
	}
	original := "exact original"
	capture := &models.SourceCapture{PostUUID: "selected-post", Metadata: models.SourcePostMetadata{Title: &original}}
	wanted := errors.New("fixture read failure")
	for _, stage := range []string{"page", "result", "missing_result", "mismatched_original"} {
		t.Run(stage, func(t *testing.T) {
			reader := newPolicyTranslationReader(t, []string{original}, 1, 1, 20)
			expected := wanted
			switch stage {
			case "page":
				reader.queryError = wanted
			case "result":
				reader.findError = wanted
			case "missing_result":
				clear(reader.results)
				expected = models.ErrSourcePayloadCorrupt
			case "mismatched_original":
				for _, result := range reader.results {
					other := "another original"
					result.OriginalText = &other
				}
				expected = models.ErrSourcePayloadCorrupt
			}
			values, complete, err := (Service{Repo: models.Repository{SourceTranslation: reader}}).sourcePostTranslations(t.Context(), capture)
			require.ErrorIs(t, err, expected)
			require.False(t, complete)
			require.Nil(t, values)
		})
	}
}

func TestPolicyTranslationObservationTimeIsUTCOrUnknown(t *testing.T) {
	unknown, err := policyTranslationTime("")
	require.NoError(t, err)
	require.Nil(t, unknown)
	second, err := policyTranslationTime("2026-10-01T23:30:00-07:00")
	require.NoError(t, err)
	require.Equal(t, "2026-10-02T06:30:00.000000000Z", *second)
	fraction, err := policyTranslationTime("2026-10-02T06:30:00.000000001Z")
	require.NoError(t, err)
	require.True(t, newerPolicyTranslation(fraction, "first", policySourceTranslation{CapturedAt: second, EvidenceUUID: "last"}))
	require.True(t, newerPolicyTranslation(second, "first", policySourceTranslation{EvidenceUUID: "last"}))
	require.False(t, newerPolicyTranslation(nil, "last", policySourceTranslation{CapturedAt: second, EvidenceUUID: "first"}))
	require.True(t, newerPolicyTranslation(second, "last", policySourceTranslation{CapturedAt: second, EvidenceUUID: "first"}))
	_, err = policyTranslationTime("not a timestamp")
	require.ErrorIs(t, err, models.ErrSourceTranslationInvalid)
}
