package sqlite

import (
	"context"

	"github.com/stashapp/stash/pkg/models"
)

// Called only when a refresh proposes removals. Other posts' current selected
// attachments and automatic whole-post links protect the union. A pinned list,
// explicit unlink or disabled selection remains authoritative for that post.
func sourceGalleryOtherMedia(ctx context.Context, gallery string, owners map[string]bool) (map[string]bool, error) {
	var aliases []string
	if err := dbWrapper.Select(ctx, &aliases, sourceAlbumGalleryAliasesQuery, gallery); err != nil {
		return nil, err
	}
	if len(aliases) > 1024 {
		return nil, models.ErrSourceAlbumLimit
	}
	q, args := sourceAlbumGalleryPostsQuery(aliases, "", 1025)
	var posts []string
	if err := dbWrapper.Select(ctx, &posts, q, args...); err != nil {
		return nil, err
	}
	if len(posts) > 1024 {
		return nil, models.ErrSourceAlbumLimit
	}
	wanted := map[string]bool{}
	for _, post := range posts {
		if owners[post] {
			continue
		}
		selection, err := (&SourceAttachmentStore{}).Selection(ctx, post)
		if err != nil {
			return nil, err
		}
		if selection == nil || selection.Decision.Mode == "disabled" {
			continue
		}
		choices, err := sourceAlbumChoices(ctx, selection)
		if err != nil {
			return nil, err
		}
		postStates, err := sourcePostMediaStates(ctx, post)
		if err != nil {
			return nil, err
		}
		requested := map[string]bool{}
		allowPostLinks := selection.Decision.Mode == "automatic"
		for _, c := range choices {
			if c.State == "unlinked" || c.State == "undecided" {
				allowPostLinks = false
			}
			if c.State == "linked" && c.MediaUUID.Valid {
				requested[c.MediaUUID.String] = true
			}
		}
		resolved, err := sourceGalleryIdentities(ctx, requested)
		if err != nil {
			return nil, err
		}
		for _, m := range resolved {
			if m.State == models.ArchiveEntityActive && postStates[m.UUID] != "unlinked" && postStates[m.UUID] != "conflict" {
				wanted[m.UUID] = true
			}
		}
		if allowPostLinks {
			for id, state := range postStates {
				if state == "linked" {
					wanted[id] = true
				}
			}
		}
	}
	return wanted, nil
}
