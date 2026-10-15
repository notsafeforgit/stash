package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

// Both directions use the existing membership indexes. A bridge album joins
// the entire component, including albums that do not overlap the starting one.
// CROSS JOIN keeps the final lookup driven by that small component instead of
// letting SQLite scan every gallery to estimate an unknown recursive result.
const overlappingSourceGalleriesQuery = `WITH RECURSIVE component(id) AS (
 SELECT id FROM galleries WHERE id=? AND origin='source'
 UNION
 SELECT b.gallery_id FROM component c JOIN galleries_images a ON a.gallery_id=c.id
 JOIN galleries_images b ON b.image_id=a.image_id JOIN galleries g ON g.id=b.gallery_id WHERE g.origin='source'
 UNION
 SELECT b.gallery_id FROM component c JOIN scenes_galleries a ON a.gallery_id=c.id
 JOIN scenes_galleries b ON b.scene_id=a.scene_id JOIN galleries g ON g.id=b.gallery_id WHERE g.origin='source'
 LIMIT 1025
)
SELECT e.uuid,e.revision,g.id AS local_id,coalesce(g.title,'') AS title
FROM component c CROSS JOIN galleries g ON g.id=c.id CROSS JOIN archive_entities e ON e.gallery_id=g.id
ORDER BY julianday(g.created_at),g.id`

func galleryConsolidationIDs(p *models.GalleryConsolidationPreview) []interface{} {
	ret := make([]interface{}, len(p.Galleries))
	for i, g := range p.Galleries {
		ret[i] = g.LocalID
	}
	return ret
}

func (s *SourceGalleryStore) PreviewConsolidation(ctx context.Context, value string) (*models.GalleryConsolidationPreview, error) {
	e, err := (&ArchiveEntityStore{}).Resolve(ctx, value)
	if err != nil {
		return nil, err
	}
	if e == nil || e.Kind != models.ArchiveGallery || e.State != models.ArchiveEntityActive {
		return nil, models.ErrSourceGalleryConflict
	}
	var rows []struct {
		UUID     string `db:"uuid"`
		Revision int    `db:"revision"`
		LocalID  int    `db:"local_id"`
		Title    string `db:"title"`
	}
	if err := dbWrapper.Select(ctx, &rows, overlappingSourceGalleriesQuery, *e.LocalID); err != nil {
		return nil, err
	}
	if len(rows) > 1024 {
		return nil, models.ErrSourceAlbumLimit
	}
	p := &models.GalleryConsolidationPreview{GalleryUUID: e.UUID, Galleries: []models.GalleryConsolidationMember{}, Conflicts: []string{}}
	for _, row := range rows {
		p.Galleries = append(p.Galleries, models.GalleryConsolidationMember{UUID: row.UUID, Revision: row.Revision, LocalID: row.LocalID, Title: row.Title})
	}
	var heads []sourceGalleryDecisionRow
	if len(rows) > 0 {
		p.GalleryUUID = rows[0].UUID
		ids := galleryConsolidationIDs(p)
		in := getInBinding(len(ids))
		if err := dbWrapper.Get(ctx, &p.ImageCount, "SELECT count(DISTINCT image_id) FROM galleries_images WHERE gallery_id IN "+in, ids...); err != nil {
			return nil, err
		}
		if err := dbWrapper.Get(ctx, &p.SceneCount, "SELECT count(DISTINCT scene_id) FROM scenes_galleries WHERE gallery_id IN "+in, ids...); err != nil {
			return nil, err
		}
		if p.ImageCount+p.SceneCount > maxSourceGalleryMembers {
			return nil, models.ErrSourceAlbumLimit
		}
		if err := dbWrapper.Select(ctx, &heads, `SELECT d.* FROM post_gallery_links l JOIN post_gallery_decisions d ON d.uuid=l.decision_uuid
JOIN archive_entities a ON a.uuid=l.gallery_uuid WHERE a.gallery_id IN `+in+` ORDER BY d.post_uuid`, ids...); err != nil {
			return nil, err
		}
		// Captions and dates remain on their source posts. Conflicting curated
		// fields require review instead of silently dropping a user's edit.
		for _, field := range []string{"code", "photographer", "studio_id", "rating"} {
			if field == "studio_id" {
				choices := map[string]bool{}
				for _, row := range rows {
					state, err := (&MetadataFieldStore{}).State(ctx, row.UUID, "studio")
					if err != nil {
						return nil, err
					}
					if state.Protected {
						choices[string(state.Value)] = true
					}
				}
				if len(choices) > 1 {
					p.Conflicts = append(p.Conflicts, field)
				}
				continue
			}
			var count int
			if err := dbWrapper.Get(ctx, &count, "SELECT count(DISTINCT "+field+") FROM galleries WHERE id IN "+in+" AND "+field+" IS NOT NULL AND "+field+"!=''", ids...); err != nil {
				return nil, err
			}
			if count > 1 {
				p.Conflicts = append(p.Conflicts, field)
			}
		}
		var custom []string
		if err := dbWrapper.Select(ctx, &custom, "SELECT field FROM gallery_custom_fields WHERE gallery_id IN "+in+" GROUP BY field HAVING count(DISTINCT value)>1 ORDER BY field", ids...); err != nil {
			return nil, err
		}
		for _, f := range custom {
			p.Conflicts = append(p.Conflicts, "custom_fields."+f)
		}
	}
	p.Signature, err = sourceSignature("stash-overlapping-galleries-v1", []any{p, heads})
	return p, err
}

func (s *SourceGalleryStore) Consolidate(ctx context.Context, value, signature string) (*models.GalleryConsolidationResult, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	p, err := s.PreviewConsolidation(ctx, value)
	if err != nil {
		return nil, err
	}
	if signature == "" || p.Signature != signature || len(p.Conflicts) > 0 {
		return nil, models.ErrSourceGalleryConflict
	}
	return s.consolidate(ctx, p)
}

func (s *SourceGalleryStore) consolidate(ctx context.Context, p *models.GalleryConsolidationPreview) (*models.GalleryConsolidationResult, error) {
	ret := &models.GalleryConsolidationResult{GalleryUUID: p.GalleryUUID, MergedUUIDs: []string{}, Added: []string{}}
	if len(p.Galleries) < 2 {
		return ret, nil
	}
	complete := false
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		if !complete {
			return models.ErrSourceGalleryConflict
		}
		var leftover bool
		if err := dbWrapper.Get(ctx, &leftover, "SELECT EXISTS(SELECT 1 FROM source_gallery_merge_context)"); err != nil {
			return err
		}
		if leftover {
			return errors.New("gallery consolidation context was not closed")
		}
		return nil
	})
	target := p.Galleries[0]
	// Preserve the effective membership choices, including exclusions for media
	// absent from the union. Physical join inserts must not become manual edits.
	heads := map[string]galleryMembershipEventRow{}
	present := map[string]bool{}
	for _, g := range p.Galleries {
		rows, err := sourceGalleryMembershipHeads(ctx, g.UUID)
		if err != nil {
			return nil, err
		}
		for _, h := range rows {
			old, ok := heads[h.MediaUUID]
			if !ok || (h.Origin != "source" && old.Origin == "source") || (h.Origin == old.Origin && h.Sequence > old.Sequence) {
				heads[h.MediaUUID] = h
			}
		}
		members, err := sourceGalleryMembers(ctx, g.LocalID)
		if err != nil {
			return nil, err
		}
		for _, m := range members {
			if g.UUID != target.UUID && !present[m.UUID] {
				ret.Added = append(ret.Added, m.UUID)
			}
			present[m.UUID] = true
		}
	}
	for _, g := range p.Galleries {
		if strings.TrimSpace(g.Title) != "" && g.Title != "Source album" {
			if _, err := dbWrapper.Exec(ctx, "INSERT OR IGNORE INTO gallery_alternate_titles(gallery_id,title) VALUES(?,?)", target.LocalID, g.Title); err != nil {
				return nil, err
			}
		}
		if g.UUID == target.UUID {
			continue
		}
		if (target.Title == "" || target.Title == "Source album") && g.Title != "" && g.Title != "Source album" {
			state, err := (&MetadataFieldStore{}).State(ctx, target.UUID, "title")
			if err != nil {
				return nil, err
			}
			if !state.Protected {
				value, err := json.Marshal(g.Title)
				if err != nil {
					return nil, err
				}
				if _, err := (&MetadataFieldStore{}).ApplyAutomatic(ctx, models.MetadataFieldDecisionInput{
					EntityUUID: target.UUID, ExpectedEntityRevision: state.Entity.Revision, Field: "title", Mode: "inherit", Origin: "policy",
					Value: value, Reason: "Use the caption title from a consolidated source album",
				}); err != nil {
					return nil, err
				}
				target.Title = g.Title
			}
		}
		if err := s.mergeSourceGallery(ctx, g, target); err != nil {
			return nil, err
		}
		ret.MergedUUIDs = append(ret.MergedUUIDs, g.UUID)
	}
	ids := make([]string, 0, len(heads))
	for id := range heads {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		h := heads[id]
		state, origin, post, selection := h.State, h.Origin, h.PostUUID, h.SelectionUUID
		if present[id] && state == "excluded" {
			// The requested union includes a member present in another album.
			state, origin, post, selection = "included", "migration", sql.NullString{}, sql.NullString{}
		}
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO gallery_membership_events(uuid,gallery_uuid,media_uuid,state,origin,post_uuid,selection_uuid)
VALUES(?,?,?,?,?,?,?)`, uuid.NewString(), target.UUID, id, state, origin, post, selection); err != nil {
			return nil, err
		}
	}
	if _, err := s.gallery.UpdatePartial(ctx, target.LocalID, models.NewGalleryPartial()); err != nil {
		return nil, err
	}
	complete = true
	return ret, nil
}

func (s *SourceGalleryStore) mergeSourceGallery(ctx context.Context, source, target models.GalleryConsolidationMember) error {
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_gallery_merge_context(source_uuid,target_uuid) VALUES(?,?)", source.UUID, target.UUID); err != nil {
		return err
	}
	var decisions []sourceGalleryDecisionRow
	if err := dbWrapper.Select(ctx, &decisions, `SELECT d.* FROM post_gallery_links l JOIN post_gallery_decisions d ON d.uuid=l.decision_uuid
JOIN source_posts p ON p.uuid=d.post_uuid WHERE l.gallery_uuid=? AND p.state='active'`, source.UUID); err != nil {
		return err
	}
	for _, d := range decisions {
		// Preserve the user's chosen origin/selection; only its gallery changes.
		if _, err := dbWrapper.Exec(ctx, "UPDATE source_posts SET revision=revision+1 WHERE uuid=?", d.PostUUID); err != nil {
			return err
		}
		id := uuid.NewString()
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO post_gallery_decisions(uuid,post_uuid,revision,state,gallery_uuid,selection_uuid,origin,reason)
SELECT ?,uuid,revision,'linked',?,?,?,'Combined source albums sharing library media' FROM source_posts WHERE uuid=?`, id, target.UUID, d.SelectionUUID, d.Origin, d.PostUUID); err != nil {
			return err
		}
		if _, err := dbWrapper.Exec(ctx, "UPDATE post_gallery_links SET decision_uuid=?,gallery_uuid=? WHERE post_uuid=?", id, target.UUID, d.PostUUID); err != nil {
			return err
		}
	}
	// Existing source records retain every caption/date. Other metadata and
	// relationships are unioned; contradictory curated fields were rejected.
	for _, relation := range []struct{ table, column string }{
		{"galleries_images", "image_id"}, {"scenes_galleries", "scene_id"}, {"performers_galleries", "performer_id"}, {"galleries_tags", "tag_id"}, {"gallery_urls", "url"}, {"gallery_alternate_titles", "title"},
	} {
		if relation.table == "performers_galleries" {
			state, err := (&MetadataFieldStore{}).State(ctx, source.UUID, "performers")
			if err != nil {
				return err
			}
			// The union of member performers is recomputed at commit. Copying
			// these derived rows as ordinary edits would freeze inheritance.
			if !state.Protected {
				continue
			}
		}
		// NOT EXISTS avoids firing BEFORE INSERT audit triggers for duplicates.
		q := "INSERT INTO " + relation.table + "(gallery_id," + relation.column + ") SELECT ?,s." + relation.column + " FROM " + relation.table + " s WHERE s.gallery_id=? AND NOT EXISTS(SELECT 1 FROM " + relation.table + " d WHERE d.gallery_id=? AND d." + relation.column + "=s." + relation.column + ")"
		if _, err := dbWrapper.Exec(ctx, q, target.LocalID, source.LocalID, target.LocalID); err != nil {
			return err
		}
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO gallery_custom_fields(gallery_id,field,value) SELECT ?,s.field,s.value FROM gallery_custom_fields s
WHERE s.gallery_id=? AND NOT EXISTS(SELECT 1 FROM gallery_custom_fields d WHERE d.gallery_id=? AND d.field=s.field)`, target.LocalID, source.LocalID, target.LocalID); err != nil {
		return err
	}
	if _, err := dbWrapper.Exec(ctx, "UPDATE galleries_chapters SET gallery_id=? WHERE gallery_id=?", target.LocalID, source.LocalID); err != nil {
		return err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO gallery_covers(gallery_id,media_uuid) SELECT ?,media_uuid FROM gallery_covers
WHERE gallery_id=? AND NOT EXISTS(SELECT 1 FROM gallery_covers WHERE gallery_id=?)`, target.LocalID, source.LocalID, target.LocalID); err != nil {
		return err
	}
	for _, field := range []string{"details", "date", "code", "photographer", "studio_id", "rating"} {
		if field == "studio_id" {
			from, err := (&MetadataFieldStore{}).State(ctx, source.UUID, "studio")
			if err != nil {
				return err
			}
			to, err := (&MetadataFieldStore{}).State(ctx, target.UUID, "studio")
			if err != nil {
				return err
			}
			if !from.Protected || to.Protected {
				continue
			}
			// A curated choice (including a clear) survives consolidation. An
			// inherited studio is recomputed from all posts at commit instead.
			input := models.MetadataFieldDecisionInput{EntityUUID: target.UUID, ExpectedEntityRevision: to.Entity.Revision,
				Field: "studio", Mode: "set", Origin: "review", Value: from.Value, Reason: "Preserved explicit studio while combining source albums"}
			if string(from.Value) == "null" {
				input.Mode, input.Value = "clear", nil
			} else {
				var studio string
				if err := json.Unmarshal(from.Value, &studio); err != nil {
					return err
				}
				e, err := (&ArchiveEntityStore{}).Find(ctx, studio)
				if err != nil {
					return err
				}
				if e == nil {
					return models.ErrSourceGalleryConflict
				}
				input.ReferenceRevisions = map[string]int{studio: e.Revision}
			}
			if _, err := (&MetadataFieldStore{}).Decide(ctx, input); err != nil {
				return err
			}
			continue
		}
		if _, err := dbWrapper.Exec(ctx, "UPDATE galleries SET "+field+"=(SELECT "+field+" FROM galleries WHERE id=?) WHERE id=? AND ("+field+" IS NULL OR "+field+"='')", source.LocalID, target.LocalID); err != nil {
			return err
		}
	}
	if _, err := dbWrapper.Exec(ctx, "UPDATE galleries SET organized=organized OR (SELECT organized FROM galleries WHERE id=?) WHERE id=?", source.LocalID, target.LocalID); err != nil {
		return err
	}
	// Redirect first so cascaded membership removal isn't mistaken for an
	// exclusion. Original UUID URLs and post associations continue resolving.
	current, err := (&ArchiveEntityStore{}).Find(ctx, source.UUID)
	if err != nil {
		return err
	}
	if err := (&ArchiveEntityStore{}).Redirect(ctx, source.UUID, target.UUID, current.Revision); err != nil {
		return err
	}
	if err := s.gallery.Destroy(ctx, source.LocalID); err != nil {
		return err
	}
	_, err = dbWrapper.Exec(ctx, "DELETE FROM source_gallery_merge_context WHERE source_uuid=?", source.UUID)
	return err
}

func (s *SourceGalleryStore) AlternateTitles(ctx context.Context, id int) ([]string, error) {
	ret := []string{}
	err := dbWrapper.Select(ctx, &ret, `SELECT title FROM gallery_alternate_titles WHERE gallery_id=?
AND title!='Source album' AND title<>coalesce((SELECT title FROM galleries WHERE id=?),'') ORDER BY title COLLATE NATURAL_CI`, id, id)
	return ret, err
}
