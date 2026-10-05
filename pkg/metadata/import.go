package metadata

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// PreparePolicyImport validates the complete retained input before considering
// current native state. It deliberately cannot execute old plugin Python/jq.
func PreparePolicyImport(input models.MetadataPolicyImportInput, now time.Time) ([]byte, string, []string, error) {
	fail := func(reason string) ([]byte, string, []string, error) {
		return nil, "", nil, fmt.Errorf("%w: %s", models.ErrMetadataPolicyImportInvalid, reason)
	}
	validUUID := func(value string) bool {
		parsed, err := uuid.Parse(value)
		return err == nil && parsed != uuid.Nil && parsed.String() == value
	}
	text := func(value string, limit int) bool {
		return value != "" && utf8.ValidString(value) && len(value) <= limit && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
	}
	doc, policy := input.Document, input.Policy
	if !validUUID(input.UUID) || !validUUID(policy.CollectionUUID) || policy.Origin != "migration" ||
		policy.ExpectedRevision < 0 || policy.ExpectedCollectionRevision <= 0 || !text(policy.Reason, 4096) {
		return fail("a stable request, native collection revision and migration reason are required")
	}
	if err := ValidateDefinition(policy.Definition); err != nil {
		return fail(err.Error())
	}
	if doc.Format != "legacy-metadata-policy" || doc.Version != 1 || !text(doc.PluginVersion, 128) ||
		len(doc.Values) == 0 || len(doc.Values) > 256 || len(doc.Values) != len(input.Dispositions) ||
		len(doc.SourceFiles) == 0 || len(doc.SourceFiles) > 32 || len(input.FolderSources) > 16 {
		return fail("unsupported source format or incomplete source inventory")
	}
	captured, err := time.Parse(time.RFC3339Nano, doc.CapturedAt)
	if err != nil || now.UnixMilli() <= 0 || now.UTC().Year() > 9999 || captured.Year() < 1970 || captured.After(now.Add(time.Minute)) {
		return fail("invalid source capture time")
	}
	for name, hash := range doc.SourceFiles {
		if !text(name, 1024) || len(hash) != 64 || strings.Trim(hash, "0123456789abcdef") != "" {
			return fail("source files require names and lowercase SHA-256 digests")
		}
	}
	review := []string{}
	for key, value := range doc.Values {
		disposition, found := input.Dispositions[key]
		if !text(key, 1024) || !json.Valid(value) || !found || !text(disposition.Reason, 4096) {
			return fail("every retained setting requires a value and an explicit disposition")
		}
		switch disposition.Action {
		case "mapped", "replaced", "retired":
		case "review":
			review = append(review, key)
		default:
			return fail("unknown setting disposition")
		}
	}
	if len(review) > 0 && policy.Definition.Enabled {
		return fail("unresolved legacy settings require a disabled native policy")
	}
	seen := map[string]bool{}
	for _, ref := range input.FolderSources {
		if !validUUID(ref.SourceUUID) || !validUUID(ref.HeadUUID) || seen[ref.SourceUUID] {
			return fail("folder sources require unique selected native document references")
		}
		seen[ref.SourceUUID] = true
	}
	body, err := json.Marshal(input)
	if err != nil || len(body) > 1<<20 {
		return fail("migration exceeds its byte limit")
	}
	sort.Strings(review)
	tree, err := archive.DecodeJSONObject(body, 1<<20)
	if err != nil {
		return fail("invalid retained JSON source values")
	}
	body, err = archive.EncodeSourceJSON(tree)
	if err != nil {
		return fail("cannot encode retained source values")
	}
	return body, fmt.Sprintf("%x", sha256.Sum256(body)), review, nil
}

func PolicyImportPlanDigest(plan models.MetadataPolicyImportPlan) (string, error) {
	plan.PlanSHA256 = ""
	body, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	tree, err := archive.DecodeJSONObject(body, 512<<10)
	if err != nil {
		return "", err
	}
	body, err = archive.EncodeSourceJSON(tree)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(body)), nil
}
