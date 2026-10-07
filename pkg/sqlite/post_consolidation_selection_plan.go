package sqlite

import (
	"context"
	"slices"

	"github.com/stashapp/stash/pkg/models"
)

func (p *postConsolidationPlan) prepareSelection(ctx context.Context, choice *models.PostConsolidationSelectionChoice) error {
	selections := p.snapshot.selections
	if choice == nil && len(selections) == 0 {
		return nil
	}
	if choice != nil && choice.Mode == "disabled" {
		p.setSelection(&models.AttachmentSelection{Decision: models.AttachmentSelectionDecision{Mode: "disabled", ManifestUUIDs: []string{}}})
		return nil
	}
	var primary *models.AttachmentSelection
	combine := false
	if choice != nil {
		primary = selections[choice.DecisionUUID]
		if primary == nil || (choice.Mode == "combine" && primary.Decision.Mode == "disabled") {
			return models.ErrAttachmentSelectionConflict
		}
		combine = choice.Mode == "combine"
	} else {
		modes := map[string]bool{}
		for _, id := range sortedPostConsolidationKeys(selections) {
			current := selections[id]
			modes[current.Decision.Mode] = true
			if primary == nil || current.Decision.PostUUID == p.snapshot.Identity.Destination.UUID {
				primary = current
			}
		}
		if len(modes) != 1 || (len(selections) > 1 && modes["pinned"]) {
			p.block("source_list_choice", "", "", "")
			return nil
		}
		combine = len(selections) > 1 && modes["automatic"]
	}
	if !combine {
		p.setSelection(primary)
		return nil
	}
	sources := map[string]selectionManifest{}
	var ids []string
	// Each original identity group contributes at most one bounded source-list
	// load. No payload reconstruction or library-wide evidence scan is needed.
	for _, root := range []string{p.snapshot.Identity.Source.UUID, p.snapshot.Identity.Destination.UUID} {
		members := map[string]bool{}
		for _, member := range p.snapshot.Identity.Members {
			if member.CanonicalUUID == root {
				members[member.UUID] = true
			}
		}
		var groupIDs []string
		for _, selected := range selections {
			if selected.Decision.Mode != "disabled" && members[selected.Decision.PostUUID] {
				groupIDs = append(groupIDs, selected.Decision.ManifestUUIDs...)
			}
		}
		slices.Sort(groupIDs)
		groupIDs = slices.Compact(groupIDs)
		group, err := loadSelectionManifests(ctx, root, groupIDs)
		if err != nil {
			return err
		}
		for id, source := range group {
			sources[id] = source
		}
		ids = append(ids, groupIDs...)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	merged, err := mergeSelection(ids, sources)
	if err != nil {
		return err
	}
	if len(merged.Conflicts) != 0 {
		p.block("source_list_conflict", "", "", "")
		return nil
	}
	decision := models.AttachmentSelectionDecision{Mode: "automatic", CaptureUUID: primary.Decision.CaptureUUID, ManifestUUIDs: ids}
	p.setSelection(selectionFromMerge(decision, merged.Manifest, sources))
	return nil
}

func (p *postConsolidationPlan) setSelection(selection *models.AttachmentSelection) {
	p.selection = selection
	p.preview.Selection = &models.PostConsolidationSelectionPlan{Mode: selection.Decision.Mode,
		CaptureUUID: selection.Decision.CaptureUUID, ManifestUUIDs: slices.Clone(selection.Decision.ManifestUUIDs),
		EntryCount: len(selection.Entries), Complete: selection.Complete, DeclaredAlbum: selection.DeclaredAlbum, ExpectedCount: selection.ExpectedCount}
}
