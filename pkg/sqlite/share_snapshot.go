package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

// FreezeTargets runs inside the admission transaction. Filters use the normal
// library query compiler, but their results stay in SQLite instead of crossing
// the API or being accumulated in the server heap.
func (s *ShareStore) FreezeTargets(ctx context.Context, share string, targets []models.ShareTarget, filters []models.ShareFilter) error {
	if _, err := getTx(ctx); err != nil {
		return err
	}
	insert := func(kind, query string, args []any, selected bool) error {
		table, column := "", ""
		switch kind {
		case "SCENE":
			table, column = "scenes", "scene_id"
		case "IMAGE":
			table, column = "images", "image_id"
		case "GALLERY":
			table, column = "galleries", "gallery_id"
		default:
			return errors.New("unsupported shared media kind")
		}
		count := "1"
		if kind == "GALLERY" {
			count = `(SELECT count(*) FROM galleries_images WHERE gallery_id=t.id)+(SELECT count(*) FROM scenes_galleries WHERE gallery_id=t.id)`
		}
		position := "offset.n+row_number() OVER(ORDER BY targets.id)"
		if selected {
			position = "targets.ordinal+1"
		}
		statement := `WITH targets AS (` + query + `), offset AS (SELECT coalesce(max(position),0) n FROM share_snapshot_entries WHERE share_id=?)
INSERT INTO share_snapshot_entries(share_id,key,position,kind,entity_id,entity_uuid,title,media_count)
SELECT ?,lower(?)||'-'||a.uuid,` + position + `,?,t.id,a.uuid,t.title,` + count + `
FROM targets JOIN ` + table + ` t ON t.id=targets.id
JOIN archive_entities a ON a.` + column + `=t.id AND a.state='active' CROSS JOIN offset`
		args = append(args, share, share, kind, kind)
		_, err := dbWrapper.Exec(ctx, statement, args...)
		return err
	}
	if len(targets) > 0 {
		seen := map[models.ShareTarget]bool{}
		for _, target := range targets {
			if target.ID < 1 || (target.Kind != "SCENE" && target.Kind != "IMAGE" && target.Kind != "GALLERY") {
				return errors.New("invalid share item")
			}
			seen[target] = true
		}
		body, err := json.Marshal(targets)
		if err != nil {
			return err
		}
		for _, kind := range []string{"SCENE", "IMAGE", "GALLERY"} {
			if err := insert(kind, `SELECT CAST(json_extract(value,'$.id') AS INTEGER) id,min(CAST(key AS INTEGER)) ordinal FROM json_each(?) WHERE json_extract(value,'$.kind')=? GROUP BY json_extract(value,'$.id')`, []any{string(body), kind}, true); err != nil {
				return err
			}
		}
		var count int
		if err := dbWrapper.Get(ctx, &count, `SELECT count(*) FROM share_snapshot_entries WHERE share_id=?`, share); err != nil {
			return err
		}
		if count != len(seen) {
			return errors.New("a selected share item is unavailable")
		}
	}
	for _, filter := range filters {
		query, _, err := shareTargetQuery(ctx, filter, 1)
		if err != nil {
			return err
		}
		if err := insert(filter.Kind, query.toSQL(false), query.allArgs(), false); err != nil {
			return err
		}
	}
	var count, empty int
	if err := dbWrapper.Get(ctx, &count, `SELECT count(*) FROM share_snapshot_entries WHERE share_id=?`, share); err != nil {
		return err
	}
	if count == 0 {
		return errors.New("a snapshot must contain at least one item")
	}
	if err := dbWrapper.Get(ctx, &empty, `SELECT count(*) FROM share_snapshot_entries WHERE share_id=? AND media_count=0`, share); err != nil {
		return err
	}
	if empty > 0 {
		return errors.New("a selected gallery has no media")
	}
	_, err := dbWrapper.Exec(ctx, `INSERT INTO share_snapshot_members(share_id,entry_key,media_key,position)
SELECT share_id,key,key,1 FROM share_snapshot_entries WHERE share_id=? AND kind<>'GALLERY'`, share)
	if err != nil {
		return err
	}
	// Preserve the library's source-aware gallery ordering. Only a bounded page
	// of gallery headers is held in Go; each membership list is inserted in SQL.
	for after := 0; ; {
		entries, err := s.snapshotEntries(ctx, share, after, 50, "", "GALLERY", false)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			break
		}
		for _, entry := range entries {
			after = entry.Position
			if entry.Kind != "GALLERY" {
				continue
			}
			order, err := gallerySourceOrder(ctx, entry.UUID)
			if err != nil {
				return err
			}
			body, err := json.Marshal(order)
			if err != nil {
				return err
			}
			_, err = dbWrapper.Exec(ctx, `WITH ordered AS MATERIALIZED (SELECT row_number() OVER() ordinal,kind,id FROM (`+galleryLibraryMediaQuery+`))
INSERT INTO share_snapshot_members(share_id,entry_key,media_key,position)
SELECT ?,?,o.kind||'-'||a.uuid,o.ordinal FROM ordered o
JOIN archive_entities a ON (o.kind='image' AND a.image_id=o.id) OR (o.kind='scene' AND a.scene_id=o.id)`, string(body), entry.ID, entry.ID, -1, 0, share, entry.Key)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// FreezeMedia copies the metadata allowlist and file lifetime in the same
// transaction as membership. No filesystem work or public URL exists yet.
func (*ShareStore) FreezeMedia(ctx context.Context, share string) error {
	_, err := dbWrapper.Exec(ctx, `WITH members AS (
 SELECT m.media_key,min(e.position) entry_position FROM share_snapshot_members m
 JOIN share_snapshot_entries e ON e.share_id=m.share_id AND e.key=m.entry_key
 WHERE m.share_id=? GROUP BY m.media_key
), ordered AS (
 SELECT members.media_key, row_number() OVER(ORDER BY members.entry_position,m.position) position
 FROM members JOIN share_snapshot_entries e ON e.share_id=? AND e.position=members.entry_position
 JOIN share_snapshot_members m ON m.share_id=e.share_id AND m.entry_key=e.key AND m.media_key=members.media_key
)
INSERT INTO share_snapshot_media(share_id,key,position,media,expected_size,expected_mtime)
SELECT ?,o.media_key,o.position,json_object(
 'uuid',a.uuid,'key',o.media_key,'kind',upper(a.kind),'entity_id',coalesce(a.scene_id,a.image_id),
 'file_id',coalesce(f.id,0),'file_uuid',coalesce(fa.uuid,''),'file_generation',coalesce(f.generation,0),
 'title',coalesce(s.title,i.title,''),'width',coalesce(v.width,img.width,0),'height',coalesce(v.height,img.height,0),
 'duration',CASE WHEN abs(v.duration)<=1.7976931348623157e308 THEN v.duration ELSE 0 END,
 'frame_rate',CASE WHEN abs(v.frame_rate)<=1.7976931348623157e308 THEN v.frame_rate ELSE 0 END,
 'video_codec',coalesce(v.video_codec,''),'audio_codec',coalesce(v.audio_codec,'')),
 coalesce(z.size,f.size,0),coalesce(unixepoch(coalesce(z.mod_time,f.mod_time)),0)
FROM ordered o JOIN archive_entities a ON a.uuid=substr(o.media_key,instr(o.media_key,'-')+1)
LEFT JOIN scenes s ON s.id=a.scene_id LEFT JOIN images i ON i.id=a.image_id
LEFT JOIN scenes_files sf ON sf.scene_id=s.id AND sf."primary"=1
LEFT JOIN images_files imf ON imf.image_id=i.id AND imf."primary"=1
LEFT JOIN files f ON f.id=coalesce(sf.file_id,imf.file_id)
LEFT JOIN archive_entities fa ON fa.file_id=f.id
LEFT JOIN files z ON z.id=f.zip_file_id
LEFT JOIN video_files v ON v.file_id=f.id LEFT JOIN image_files img ON img.file_id=f.id`, share, share, share)
	if err != nil {
		return err
	}
	_, err = dbWrapper.Exec(ctx, `UPDATE shares SET preparation_revision=1,media_count=(SELECT count(*) FROM share_snapshot_media WHERE share_id=?),selection_count=(SELECT count(*) FROM share_snapshot_entries WHERE share_id=?) WHERE id=?`, share, share, share)
	return err
}

func (*ShareStore) Preparation(ctx context.Context, share string, limit int) ([]models.SharePreparedMedia, error) {
	ret := []models.SharePreparedMedia{}
	err := dbWrapper.Select(ctx, &ret, `SELECT position,media,expected_size,expected_mtime FROM share_snapshot_media WHERE share_id=? AND position>(SELECT prepared_count FROM shares WHERE id=?) ORDER BY position LIMIT ?`, share, share, limit)
	return ret, err
}

func (*ShareStore) NextPreparation(ctx context.Context, now int64) (*models.ShareRecord, error) {
	var row models.ShareRecord
	err := dbWrapper.Get(ctx, &row, `SELECT * FROM shares WHERE preparation_state='PREPARING' AND revoked_at=0 AND expires_at>? ORDER BY created_at,id LIMIT 1`, now)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &row, err
}

func (*ShareStore) CheckpointPreparation(ctx context.Context, row *models.ShareRecord, media []models.ShareMedia, failure string) (bool, error) {
	state := "PREPARING"
	if failure != "" {
		state = "FAILED"
	} else if row.PreparedCount+len(media) == row.MediaCount {
		state = "READY"
	}
	// The generation makes concurrent preparation and retry harmless. Update
	// media first, but only if this exact preparation revision is still active.
	var current bool
	if err := dbWrapper.Get(ctx, &current, `SELECT EXISTS(SELECT 1 FROM shares WHERE id=? AND preparation_revision=? AND preparation_state='PREPARING' AND revoked_at=0 AND expires_at>?)`, row.ID, row.PreparationRevision, time.Now().Unix()); err != nil {
		return false, err
	}
	if !current {
		return false, nil
	}
	for _, item := range media {
		body, err := json.Marshal(item.Fingerprint)
		if err != nil {
			return false, err
		}
		if _, err := dbWrapper.Exec(ctx, `UPDATE share_snapshot_media SET media=json_set(media,'$.fingerprint',json(?)),prepared=1 WHERE share_id=? AND key=? AND prepared=0`, string(body), row.ID, item.Key); err != nil {
			return false, err
		}
	}
	_, err := dbWrapper.Exec(ctx, `UPDATE shares SET prepared_count=prepared_count+?,preparation_revision=preparation_revision+1,preparation_state=?,preparation_error=? WHERE id=?`, len(media), state, failure, row.ID)
	return true, err
}

func (*ShareStore) RetryPreparation(ctx context.Context, share string) error {
	_, err := dbWrapper.Exec(ctx, `UPDATE shares SET preparation_state='PREPARING',preparation_error='',preparation_revision=preparation_revision+1 WHERE id=? AND preparation_state='FAILED' AND revoked_at=0 AND expires_at>?`, share, time.Now().Unix())
	return err
}

const activeSnapshotEntry = `EXISTS(SELECT 1 FROM archive_entities a WHERE a.state='active'
 AND a.kind=lower(e.kind) AND (e.entity_uuid='' OR a.uuid=e.entity_uuid)
 AND ((e.kind='SCENE' AND a.scene_id=e.entity_id) OR (e.kind='IMAGE' AND a.image_id=e.entity_id) OR (e.kind='GALLERY' AND a.gallery_id=e.entity_id)))`

func (s *ShareStore) SnapshotEntries(ctx context.Context, share string, after, limit int, key, kind string) ([]models.ShareEntry, error) {
	return s.snapshotEntries(ctx, share, after, limit, key, kind, true)
}

func (*ShareStore) snapshotEntries(ctx context.Context, share string, after, limit int, key, kind string, active bool) ([]models.ShareEntry, error) {
	var rows []struct {
		Position               int
		Key, Kind, UUID, Title string
		ID                     int
		MediaCount             int `db:"media_count"`
	}
	query := `SELECT e.position,e.key,e.kind,e.entity_id id,e.entity_uuid uuid,e.title,e.media_count FROM share_snapshot_entries e WHERE e.share_id=? AND e.position>?`
	args := []any{share, after}
	if kind != "" {
		query += ` AND e.kind=?`
		args = append(args, kind)
	}
	if key != "" {
		query += ` AND e.key=?`
		args = append(args, key)
	}
	if active {
		query += ` AND ` + activeSnapshotEntry
	}
	query += ` ORDER BY e.position LIMIT ?`
	args = append(args, limit)
	if err := dbWrapper.Select(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	ret := []models.ShareEntry{}
	for _, r := range rows {
		ret = append(ret, models.ShareEntry{Position: r.Position, Key: r.Key, Kind: r.Kind, ID: r.ID, UUID: r.UUID, Title: r.Title, MediaCount: r.MediaCount, MediaKeys: []string{}})
	}
	return ret, nil
}

func (*ShareStore) SnapshotMedia(ctx context.Context, share string, after, limit int, gallery, kind string) ([]models.SharePreparedMedia, error) {
	ret := []models.SharePreparedMedia{}
	query := `SELECT m.position,m.media,m.expected_size,m.expected_mtime FROM share_snapshot_media m WHERE m.share_id=? AND m.position>? AND json_extract(m.media,'$.kind')=? AND EXISTS(
 SELECT 1 FROM share_snapshot_members sm JOIN share_snapshot_entries e ON e.share_id=sm.share_id AND e.key=sm.entry_key
 WHERE sm.share_id=m.share_id AND sm.media_key=m.key AND ` + activeSnapshotEntry + `) ORDER BY m.position LIMIT ?`
	args := []any{share, after, kind, limit}
	if gallery != "" {
		query = `SELECT sm.position,m.media,m.expected_size,m.expected_mtime FROM share_snapshot_members sm
 JOIN share_snapshot_entries e ON e.share_id=sm.share_id AND e.key=sm.entry_key
 JOIN share_snapshot_media m ON m.share_id=sm.share_id AND m.key=sm.media_key
 WHERE sm.share_id=? AND sm.entry_key=? AND sm.position>? AND ` + activeSnapshotEntry + ` ORDER BY sm.position LIMIT ?`
		args = []any{share, gallery, after, limit}
	}
	err := dbWrapper.Select(ctx, &ret, query, args...)
	return ret, err
}

func (*ShareStore) SnapshotItem(ctx context.Context, share, key string) (*models.ShareMedia, error) {
	var body []byte
	err := dbWrapper.Get(ctx, &body, `SELECT m.media FROM share_snapshot_media m WHERE m.share_id=? AND m.key=? AND EXISTS(
 SELECT 1 FROM share_snapshot_members sm JOIN share_snapshot_entries e ON e.share_id=sm.share_id AND e.key=sm.entry_key
 WHERE sm.share_id=m.share_id AND sm.media_key=m.key AND `+activeSnapshotEntry+`)`, share, key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ret models.ShareMedia
	return &ret, json.Unmarshal(body, &ret)
}

func (*ShareStore) SnapshotEntryKeys(ctx context.Context, share, entry string, keys []string) ([]string, error) {
	ret := []string{}
	if len(keys) == 0 {
		return ret, nil
	}
	args := []any{share, entry}
	for _, key := range keys {
		args = append(args, key)
	}
	query := fmt.Sprintf(`SELECT media_key FROM share_snapshot_members WHERE share_id=? AND entry_key=? AND media_key IN (%s) ORDER BY position`, strings.TrimSuffix(strings.Repeat("?,", len(keys)), ","))
	err := dbWrapper.Select(ctx, &ret, query, args...)
	return ret, err
}
