package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

const maxPolicyTranslations = 128
const maxPolicyTranslationEvidence = 4096
const maxPolicyTranslationBytes = 1 << 20

// A result shared by title and caption appears once. The fields identify exact
// original-text matches without repeating the original text or capture body.
// EvidenceUUID links to the most recently observed assertion of this result;
// full history and original timestamps remain in source translation storage.
type policySourceTranslation struct {
	UUID           string   `json:"uuid"`
	SourceFields   []string `json:"source_fields"`
	TranslatedText string   `json:"translated_text"`
	SourceLanguage *string  `json:"source_language"`
	TargetLanguage *string  `json:"target_language"`
	Provider       *string  `json:"provider"`
	EvidenceUUID   string   `json:"evidence_uuid"`
	CapturedAt     *string  `json:"captured_at"`
}

// Only this post and the selected capture's exact title/caption are considered.
// Unknown originals cannot establish a match. Language/provider facts remain
// nullable; a mapping must explicitly choose its desired language and policy.
func (s Service) sourcePostTranslations(ctx context.Context, capture *models.SourceCapture) ([]policySourceTranslation, bool, error) {
	values := []policySourceTranslation{}
	positions := make(map[string]int)
	originals := []string{}
	for _, text := range []*string{capture.Metadata.Title, capture.Metadata.OriginalText} {
		if text != nil && *text != "" && (len(originals) == 0 || originals[0] != *text) {
			originals = append(originals, *text)
		}
	}
	read, size := 0, 2
	for _, original := range originals {
		sum := sha256.Sum256([]byte(original))
		after := ""
		for {
			rows, err := s.Repo.SourceTranslation.PostEvidence(ctx, models.SourceTranslationQuery{
				PostUUID: capture.PostUUID, OriginalSHA256: hex.EncodeToString(sum[:]), After: after, Limit: 100,
			})
			if err != nil {
				return nil, false, err
			}
			for _, row := range rows {
				read++
				if read > maxPolicyTranslationEvidence {
					return nil, false, nil
				}
				if row.PostUUID != capture.PostUUID {
					return nil, false, models.ErrSourcePayloadCorrupt
				}
				observed, err := policyTranslationTime(row.CapturedAt)
				if err != nil {
					return nil, false, err
				}
				position, exists := positions[row.TranslationUUID]
				var candidate policySourceTranslation
				previousSize := 0
				if exists {
					candidate = values[position]
					if !newerPolicyTranslation(observed, row.UUID, candidate) {
						continue
					}
					encodedSize, err := policyTranslationSize(candidate, true)
					if err != nil {
						return nil, false, err
					}
					previousSize = encodedSize
				} else {
					if len(values) == maxPolicyTranslations {
						return nil, false, nil
					}
					result, err := s.Repo.SourceTranslation.Find(ctx, row.TranslationUUID)
					if err != nil {
						return nil, false, err
					}
					if result == nil || result.UUID != row.TranslationUUID || result.OriginalText == nil || *result.OriginalText != original {
						return nil, false, models.ErrSourcePayloadCorrupt
					}
					fields := []string{}
					if capture.Metadata.Title != nil && *capture.Metadata.Title == original {
						fields = append(fields, "title")
					}
					if capture.Metadata.OriginalText != nil && *capture.Metadata.OriginalText == original {
						fields = append(fields, "original_text")
					}
					candidate = policySourceTranslation{UUID: result.UUID, SourceFields: fields, TranslatedText: result.TranslatedText,
						SourceLanguage: result.SourceLanguage, TargetLanguage: result.TargetLanguage, Provider: result.Provider}
				}
				candidate.EvidenceUUID, candidate.CapturedAt = row.UUID, observed
				encodedSize, err := policyTranslationSize(candidate, exists)
				if err != nil {
					return nil, false, err
				}
				size += encodedSize - previousSize
				if size > maxPolicyTranslationBytes {
					return nil, false, nil
				}
				if exists {
					values[position] = candidate
				} else {
					positions[candidate.UUID] = len(values)
					values = append(values, candidate)
				}
			}
			if len(rows) < 100 {
				break
			}
			after = rows[len(rows)-1].UUID
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].UUID < values[j].UUID })
	return values, true, nil
}

func policyTranslationSize(value policySourceTranslation, referenceOnly bool) (int, error) {
	var input any = value
	if referenceOnly {
		// Repeated evidence changes only this small reference. Do not re-encode
		// a potentially large shared text body for every observation.
		input = [2]any{value.EvidenceUUID, value.CapturedAt}
	}
	encoded, err := json.Marshal(input)
	return len(encoded) + 1, err
}

func policyTranslationTime(value string) (*string, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.UTC().Year() < 0 || parsed.UTC().Year() > 9999 {
		return nil, models.ErrSourceTranslationInvalid
	}
	// Fixed precision makes jq string ordering chronological, including a whole
	// second versus a fractional second. Never substitute the import time.
	canonical := parsed.UTC().Format("2006-01-02T15:04:05.000000000Z")
	return &canonical, nil
}

func newerPolicyTranslation(observed *string, evidence string, current policySourceTranslation) bool {
	if observed == nil || current.CapturedAt == nil {
		if (observed == nil) != (current.CapturedAt == nil) {
			return observed != nil
		}
	} else if *observed != *current.CapturedAt {
		return *observed > *current.CapturedAt
	}
	return evidence > current.EvidenceUUID
}
