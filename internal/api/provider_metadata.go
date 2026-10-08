package api

import (
	"context"
	"fmt"
	"slices"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/stashbox"
)

// recordProviderSelections must run after the edit, in its transaction. The
// input identifies user-selected sources; values always come from the database.
// A receipt is historical evidence of that choice, not remote-signed metadata.
func (r *mutationResolver) recordProviderSelections(ctx context.Context, kind models.ArchiveEntityKind, id int, selections []*models.ProviderMetadataSelectionInput, translator changesetTranslator) error {
	if len(selections) > 32 {
		return fmt.Errorf("%w: too many provider selections", models.ErrProviderMetadataInvalid)
	}
	sources := make(map[[2]string]bool, len(selections))
	for _, selection := range selections {
		if selection == nil || len(selection.Fields) == 0 {
			return models.ErrProviderMetadataInvalid
		}
		key := [2]string{selection.Endpoint, selection.RemoteID}
		if sources[key] {
			return fmt.Errorf("%w: duplicate provider selection", models.ErrProviderMetadataInvalid)
		}
		sources[key] = true
		for _, field := range selection.Fields {
			if !slices.Contains(models.ProviderMetadataFields(kind), field) || !providerFieldIncluded(kind, field, translator) {
				return fmt.Errorf("%w: selected field %q must be metadata included in this edit", models.ErrProviderMetadataInvalid, field)
			}
		}
		if err := stashbox.RecordMetadataImport(ctx, r.repository, kind, id, selection.Endpoint, &selection.RemoteID, "review", selection.Fields); err != nil {
			return err
		}
	}
	return nil
}

func providerFieldIncluded(kind models.ArchiveEntityKind, field string, translator changesetTranslator) bool {
	switch field {
	case "image":
		if kind == models.ArchiveScene {
			return translator.hasField("cover_image")
		}
		return translator.hasField("image") || translator.hasField("image_input")
	case "height":
		return translator.hasField("height_cm")
	case "performers":
		return translator.hasField("performer_ids")
	case "studio":
		return translator.hasField("studio_id")
	case "tags":
		return translator.hasField("tag_ids")
	case "parent":
		return translator.hasField("parent_id")
	case "parents":
		return translator.hasField("parent_ids")
	case "aliases":
		return translator.hasField("aliases") || (kind == models.ArchivePerformer && translator.hasField("alias_list"))
	case "urls":
		return translator.hasField("urls") || translator.hasField("url")
	default:
		return translator.hasField(field)
	}
}
