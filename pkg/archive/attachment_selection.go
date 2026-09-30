package archive

import (
	"cmp"
	"errors"
	"slices"

	"github.com/stashapp/stash/pkg/models"
)

const MaxSelectionManifests = MaxManifestEntries + 3 // one contributor per position plus list-level facts
const MaxSelectionSourceEntries = MaxManifestEntries * 4

type AttachmentManifestMerge struct {
	Manifest  models.SourceAttachmentManifestInput
	Conflicts []models.AttachmentManifestConflict
	// Input indexes still needed to prove the combined list. Redundant source
	// lists remain in capture history but need not be attached to every choice.
	Contributors []int
}

// MergeAttachmentManifests is monotonic: compatible partial lists add evidence,
// and disagreements stay explicit. It never treats capture time as permission
// to remove or reorder known attachments. Input order only breaks ties between
// equivalent evidence; put previously selected lists first to avoid churn.
func MergeAttachmentManifests(inputs []models.SourceAttachmentManifestInput) (*AttachmentManifestMerge, error) {
	if len(inputs) == 0 || len(inputs) > MaxSelectionManifests+1 {
		return nil, errors.New("attachment selection has an invalid number of source lists")
	}
	ret := &AttachmentManifestMerge{}
	entries := make(map[int]models.SourceAttachmentEntry)
	coverage, typed := make(map[int]int), make(map[int]int)
	complete, albums, counts, total := 0, 0, 0, 0
	inputs = slices.Clone(inputs)
	for i, input := range inputs {
		var err error
		input, err = NormalizeAttachmentManifest(input)
		if err != nil {
			return nil, err
		}
		inputs[i] = input
		total += len(input.Entries)
		if total > MaxSelectionSourceEntries {
			return nil, errors.New("attachment selection exceeds its source-entry budget")
		}
		if input.Complete {
			complete++
		}
		if input.DeclaredAlbum {
			albums++
		}
		if input.ExpectedCount != nil {
			counts++
			if ret.Manifest.ExpectedCount != nil && *ret.Manifest.ExpectedCount != *input.ExpectedCount {
				ret.Conflicts = append(ret.Conflicts, models.AttachmentManifestConflict{Kind: "count"})
			} else {
				ret.Manifest.ExpectedCount = input.ExpectedCount
			}
		}
		for _, entry := range input.Entries {
			old, found := entries[entry.Position]
			switch {
			case found && old.Reference != entry.Reference:
				position := entry.Position
				ret.Conflicts = append(ret.Conflicts, models.AttachmentManifestConflict{Kind: "position", Position: &position})
			case found && old.MediaKind != "unknown" && entry.MediaKind != "unknown" && old.MediaKind != entry.MediaKind:
				position := entry.Position
				ret.Conflicts = append(ret.Conflicts, models.AttachmentManifestConflict{Kind: "media-kind", Position: &position})
			case !found || old.MediaKind == "unknown":
				entries[entry.Position] = entry
			}
			coverage[entry.Position]++
			if entry.MediaKind != "unknown" {
				typed[entry.Position]++
			}
		}
	}
	ret.Manifest.Complete, ret.Manifest.DeclaredAlbum = complete > 0, albums > 0
	for _, entry := range entries {
		if ret.Manifest.ExpectedCount != nil && entry.Position >= *ret.Manifest.ExpectedCount {
			position := entry.Position
			ret.Conflicts = append(ret.Conflicts, models.AttachmentManifestConflict{Kind: "count-position", Position: &position})
		}
		ret.Manifest.Entries = append(ret.Manifest.Entries, entry)
	}
	if len(ret.Conflicts) > 0 {
		// Do not expose a plausible-looking partial result when evidence conflicts.
		ret.Manifest = models.SourceAttachmentManifestInput{}
		slices.SortFunc(ret.Conflicts, func(a, b models.AttachmentManifestConflict) int {
			if a.Kind != b.Kind {
				return cmp.Compare(a.Kind, b.Kind)
			}
			if a.Position == nil || b.Position == nil {
				return 0
			}
			return cmp.Compare(*a.Position, *b.Position)
		})
		ret.Conflicts = slices.CompactFunc(ret.Conflicts, func(a, b models.AttachmentManifestConflict) bool {
			return a.Kind == b.Kind && ((a.Position == nil && b.Position == nil) || (a.Position != nil && b.Position != nil && *a.Position == *b.Position))
		})
		return ret, nil
	}
	var err error
	ret.Manifest, err = NormalizeAttachmentManifest(ret.Manifest)
	if err != nil {
		return nil, err
	}
	// Remove redundant lists from the end, keeping older equivalent support.
	// Coverage counters avoid repeatedly reassembling every possible subset.
	remaining := len(inputs)
	for i := len(inputs) - 1; i >= 0; i-- {
		input := inputs[i]
		redundant := (!input.Complete || complete > 1) && (!input.DeclaredAlbum || albums > 1) && (input.ExpectedCount == nil || counts > 1)
		for _, entry := range input.Entries {
			if coverage[entry.Position] < 2 || (entry.MediaKind != "unknown" && typed[entry.Position] < 2) {
				redundant = false
				break
			}
		}
		// Keep one source even for a confirmed empty list or an unknown count.
		if redundant && remaining > 1 {
			remaining--
			if input.Complete {
				complete--
			}
			if input.DeclaredAlbum {
				albums--
			}
			if input.ExpectedCount != nil {
				counts--
			}
			for _, entry := range input.Entries {
				coverage[entry.Position]--
				if entry.MediaKind != "unknown" {
					typed[entry.Position]--
				}
			}
		} else {
			ret.Contributors = append(ret.Contributors, i)
		}
	}
	if len(ret.Contributors) == 0 {
		ret.Contributors = []int{0}
	}
	slices.Reverse(ret.Contributors)
	if len(ret.Contributors) > MaxSelectionManifests {
		return nil, errors.New("attachment selection has too many contributors")
	}
	return ret, nil
}
