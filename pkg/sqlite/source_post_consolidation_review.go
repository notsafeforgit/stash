package sqlite

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type postConsolidationChoiceConflict struct {
	Kind      string   `json:"kind"`
	PostUUIDs []string `json:"post_uuids"`
	Namespace string   `json:"namespace,omitempty"`
	Value     string   `json:"value,omitempty"`
	MediaUUID string   `json:"media_uuid,omitempty"`
	Position  *int     `json:"position,omitempty"`
}

// The review must inspect every member of both canonical groups. Inspecting
// just the two roots can hide a retained unlink or a different album on an
// earlier alias. Original choices remain independent evidence until the apply
// operation explicitly resolves them in the same transaction as consolidation.
type postConsolidationChoiceSnapshot struct {
	Identity  *postIdentitySnapshot
	Posts     []models.SourcePostComparisonState
	Conflicts []postConsolidationChoiceConflict
	Signature string
}

const postConsolidationManifestBudgetQuery = `SELECT m.entry_count FROM source_post_identities i
JOIN post_attachment_selections s ON s.post_uuid=i.post_uuid
JOIN post_attachment_decision_manifests d ON d.decision_uuid=s.decision_uuid
JOIN source_attachment_manifests m ON m.uuid=d.manifest_uuid
WHERE i.canonical_uuid IN (?,?) LIMIT ?`

func inspectPostConsolidationChoices(ctx context.Context, source, destination string) (*postConsolidationChoiceSnapshot, error) {
	identity, err := inspectPostConsolidationIdentity(ctx, source, destination)
	if err != nil {
		return nil, err
	}
	var manifestCounts []int
	if err := dbWrapper.Select(ctx, &manifestCounts, postConsolidationManifestBudgetQuery, source, destination, archive.MaxSelectionManifests+1); err != nil {
		return nil, err
	}
	if len(manifestCounts) > archive.MaxSelectionManifests {
		return nil, models.ErrSourcePostIdentityLimit
	}
	totalSourceEntries := 0
	for _, count := range manifestCounts {
		totalSourceEntries += count
		if totalSourceEntries > archive.MaxSelectionSourceEntries {
			return nil, models.ErrSourcePostIdentityLimit
		}
	}
	ret := &postConsolidationChoiceSnapshot{Identity: identity, Posts: []models.SourcePostComparisonState{}, Conflicts: []postConsolidationChoiceConflict{}}
	var references, attachments, mediaChoices, sourceEntries int
	selections := make([]*models.AttachmentSelection, 0, len(identity.Members))
	store := &SourceEvidenceStore{}
	for _, member := range identity.Members {
		post, selection, err := store.postComparisonState(ctx, member.UUID)
		if err != nil {
			return nil, err
		}
		references += len(post.Identifiers) + len(post.URLs)
		attachments += len(post.Attachments)
		mediaChoices += len(post.MediaChoices)
		if selection != nil {
			sourceEntries += len(selection.Entries)
			selections = append(selections, selection)
		}
		if references > maxPostIdentityIdentifiers || attachments > maxPostComparisonChoices || mediaChoices > maxPostComparisonChoices || sourceEntries > archive.MaxSelectionSourceEntries {
			return nil, models.ErrSourcePostIdentityLimit
		}
		ret.Posts = append(ret.Posts, *post)
	}
	if err := postConsolidationSelectionConflicts(ret, selections); err != nil {
		return nil, err
	}
	postConsolidationAssociationConflicts(ret)
	ret.Signature, err = sourceSignature("stash-post-consolidation-choices-v1", ret)
	return ret, err
}

func postConsolidationSelectionConflicts(ret *postConsolidationChoiceSnapshot, selections []*models.AttachmentSelection) error {
	modes := map[string]bool{}
	posts := []string{}
	inputs := []models.SourceAttachmentManifestInput{}
	for _, selection := range selections {
		modes[selection.Decision.Mode] = true
		posts = append(posts, selection.Decision.PostUUID)
		if selection.Decision.Mode == "disabled" {
			continue
		}
		input := models.SourceAttachmentManifestInput{Complete: selection.Complete, DeclaredAlbum: selection.DeclaredAlbum, ExpectedCount: selection.ExpectedCount}
		for _, entry := range selection.Entries {
			input.Entries = append(input.Entries, models.SourceAttachmentEntry{Position: entry.Position, MediaKind: entry.MediaKind, Reference: entry.Attachment.Reference})
		}
		inputs = append(inputs, input)
	}
	if len(modes) > 1 {
		ret.Conflicts = append(ret.Conflicts, postConsolidationChoiceConflict{Kind: "source_list_mode", PostUUIDs: posts})
	}
	if len(inputs) < 2 {
		return nil
	}
	merged, err := archive.MergeAttachmentManifests(inputs)
	if err != nil {
		return err
	}
	for _, conflict := range merged.Conflicts {
		ret.Conflicts = append(ret.Conflicts, postConsolidationChoiceConflict{Kind: "source_list_" + strings.ReplaceAll(conflict.Kind, "-", "_"), PostUUIDs: posts, Position: conflict.Position})
	}
	return nil
}

type postConsolidationChoiceGroup struct {
	choices map[string]bool
	posts   map[string]bool
}

func addPostConsolidationChoice(groups map[string]*postConsolidationChoiceGroup, key, choice, post string) {
	if groups[key] == nil {
		groups[key] = &postConsolidationChoiceGroup{choices: map[string]bool{}, posts: map[string]bool{}}
	}
	groups[key].choices[choice], groups[key].posts[post] = true, true
}

func sortedPostConsolidationKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func postConsolidationAssociationConflicts(ret *postConsolidationChoiceSnapshot) {
	albums, media := map[string]*postConsolidationChoiceGroup{}, map[string]*postConsolidationChoiceGroup{}
	attachments := map[models.SourcePostIdentifier]*postConsolidationChoiceGroup{}
	for _, post := range ret.Posts {
		if post.Album != nil {
			choice := post.Album.State
			if post.Album.Gallery != nil {
				choice += ":" + post.Album.Gallery.UUID
				if post.Album.Gallery.State != models.ArchiveEntityActive {
					ret.Conflicts = append(ret.Conflicts, postConsolidationChoiceConflict{Kind: "gallery_unavailable", PostUUIDs: []string{post.UUID}, MediaUUID: post.Album.Gallery.UUID})
				}
			}
			addPostConsolidationChoice(albums, "album", choice, post.UUID)
		}
		for _, choice := range post.MediaChoices {
			addPostConsolidationChoice(media, choice.Media.UUID, choice.Decision.State, post.UUID)
		}
		for _, attachment := range post.Attachments {
			if attachment.Choice == nil {
				continue
			}
			key := models.SourcePostIdentifier{Namespace: attachment.Namespace, Value: attachment.Value}
			if attachments[key] == nil {
				attachments[key] = &postConsolidationChoiceGroup{choices: map[string]bool{}, posts: map[string]bool{}}
			}
			choice := attachment.Choice.State
			if attachment.Media != nil {
				choice += ":" + attachment.Media.UUID
			}
			attachments[key].choices[choice], attachments[key].posts[post.UUID] = true, true
		}
	}
	if group := albums["album"]; group != nil && len(group.choices) > 1 {
		ret.Conflicts = append(ret.Conflicts, postConsolidationChoiceConflict{Kind: "gallery_choice", PostUUIDs: sortedPostConsolidationKeys(group.posts)})
	}
	for _, id := range sortedPostConsolidationKeys(media) {
		if group := media[id]; len(group.choices) > 1 {
			ret.Conflicts = append(ret.Conflicts, postConsolidationChoiceConflict{Kind: "media_choice", PostUUIDs: sortedPostConsolidationKeys(group.posts), MediaUUID: id})
		}
	}
	keys := make([]models.SourcePostIdentifier, 0, len(attachments))
	for key := range attachments {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b models.SourcePostIdentifier) int {
		if result := cmp.Compare(a.Namespace, b.Namespace); result != 0 {
			return result
		}
		return cmp.Compare(a.Value, b.Value)
	})
	for _, key := range keys {
		if group := attachments[key]; len(group.choices) > 1 {
			ret.Conflicts = append(ret.Conflicts, postConsolidationChoiceConflict{Kind: "attachment_choice", PostUUIDs: sortedPostConsolidationKeys(group.posts), Namespace: key.Namespace, Value: key.Value})
		}
	}
}
