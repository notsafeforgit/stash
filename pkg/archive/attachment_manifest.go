package archive

import (
	"cmp"
	"errors"
	"slices"

	"github.com/stashapp/stash/pkg/models"
)

const AttachmentManifestVersion = "attachments-v1"
const MaxManifestEntries = 4096
const MaxManifestPositions = 1000000
const MaxManifestBytes = 4 << 20

// NormalizeAttachmentManifest keeps incomplete source lists distinct from
// incomplete downloads. It never infers entries from filenames or directories.
func NormalizeAttachmentManifest(input models.SourceAttachmentManifestInput) (models.SourceAttachmentManifestInput, error) {
	if len(input.Entries) > MaxManifestEntries {
		return input, errors.New("source attachment manifest exceeds 4096 entries")
	}
	if input.ExpectedCount != nil {
		count := *input.ExpectedCount
		if count < 0 || count > MaxManifestPositions {
			return input, errors.New("invalid source attachment count")
		}
		input.ExpectedCount = &count
	}
	input.Entries = slices.Clone(input.Entries)
	slices.SortFunc(input.Entries, func(a, b models.SourceAttachmentEntry) int { return cmp.Compare(a.Position, b.Position) })
	for i, entry := range input.Entries {
		if entry.Position < 0 || entry.Position >= MaxManifestPositions || (i > 0 && input.Entries[i-1].Position == entry.Position) ||
			(input.ExpectedCount != nil && entry.Position >= *input.ExpectedCount) {
			return input, errors.New("invalid or duplicate source attachment position")
		}
		if !ValidAccountNamespace(entry.Reference.Namespace) {
			return input, errors.New("invalid source attachment namespace")
		}
		if _, err := NormalizeAccountReference(models.AccountReference{Namespace: entry.Reference.Namespace, Kind: "id", Value: entry.Reference.Value}); err != nil {
			return input, err
		}
		if entry.MediaKind != "image" && entry.MediaKind != "video" && entry.MediaKind != "unknown" {
			return input, errors.New("invalid source attachment media kind")
		}
		if input.Complete && entry.Position != i {
			return input, errors.New("complete source attachment manifest has missing positions")
		}
	}
	if input.Complete {
		count := len(input.Entries)
		if input.ExpectedCount != nil && *input.ExpectedCount != count {
			return input, errors.New("complete source attachment manifest does not match its declared count")
		}
		input.ExpectedCount = &count
	}
	encoded, err := EncodeSourceJSON(AttachmentManifestSignatureInput(input))
	if err != nil {
		return input, err
	}
	if len(encoded) > MaxManifestBytes {
		return input, errors.New("source attachment manifest exceeds 4 MiB")
	}
	return input, nil
}

// AttachmentManifestSignatureInput uses only source identifiers, not newly
// allocated database UUIDs, so repeated captures reuse one ordered snapshot.
func AttachmentManifestSignatureInput(input models.SourceAttachmentManifestInput) []interface{} {
	entries := make([][]interface{}, 0, len(input.Entries))
	for _, entry := range input.Entries {
		entries = append(entries, []interface{}{entry.Position, entry.Reference.Namespace, entry.Reference.Value, entry.MediaKind})
	}
	return []interface{}{AttachmentManifestVersion, input.Complete, input.DeclaredAlbum, input.ExpectedCount, entries}
}
