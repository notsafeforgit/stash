package sqlite

import (
	"context"
	"database/sql"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

const twitterFilenameNamespace = "legacy:twitter:filename"
const twitterFilenameReason = "Recovered partial Twitter attachment order from original filenames; total count unknown"

var twitterOriginalFilename = regexp.MustCompile(`^([1-9][0-9]{0,31})_([1-9][0-9]{0,5})\.(?i:jpg|jpeg|png|webp|gif|avif|heic|heif|mp4|m4v|webm|mov|mkv)$`)

// Discovery includes unselected posts: the old album discovery necessarily
// omitted precisely the historical posts whose source lists need recovery.
func (s *SourceGalleryStore) FilenameBackfillPosts(ctx context.Context, after string, limit int) ([]models.SelectedSourcePost, error) {
	if after != "" {
		if id, err := archiveUUID(after); err != nil || id != after {
			return nil, models.ErrSourceAlbumPolicy
		}
	}
	if limit < 1 || limit > 100 {
		return nil, models.ErrSourceAlbumLimit
	}
	var rows []struct {
		PostUUID      string `db:"post_uuid"`
		PostState     string `db:"post_state"`
		SelectionUUID string `db:"selection_uuid"`
		Mode          string `db:"mode"`
	}
	err := dbWrapper.Select(ctx, &rows, `SELECT p.uuid AS post_uuid,p.state AS post_state,
 coalesce(d.uuid,'') AS selection_uuid,coalesce(d.mode,'unselected') AS mode
 FROM source_posts p LEFT JOIN post_attachment_selections s ON s.post_uuid=p.uuid
 LEFT JOIN post_attachment_decisions d ON d.uuid=s.decision_uuid
 WHERE p.uuid>? AND p.state='active'
 AND EXISTS(SELECT 1 FROM source_post_identities i
  JOIN source_post_file_evidence e ON e.post_uuid=i.post_uuid
  JOIN source_file_observations o ON o.uuid=e.observation_uuid
  JOIN source_post_identities keys ON keys.canonical_uuid=i.canonical_uuid
  JOIN source_post_identifiers k ON k.post_uuid=keys.post_uuid AND k.namespace='native:twitter'
  WHERE i.canonical_uuid=p.uuid AND e.origin='migration' AND e.basis='catalog-appearance'
  AND (('/'||o.relative_path) GLOB ('*/'||k.value||'_[2-9].*')
   OR ('/'||o.relative_path) GLOB ('*/'||k.value||'_[1-9][0-9]*.*')))
 ORDER BY p.uuid LIMIT ?`, after, limit)
	if err != nil {
		return nil, err
	}
	ret := make([]models.SelectedSourcePost, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, models.SelectedSourcePost{PostUUID: row.PostUUID, PostState: row.PostState, SelectionUUID: row.SelectionUUID, Mode: row.Mode})
	}
	return ret, nil
}

// Only the original imported name is evidence. A converted or deduplicated
// survivor may have a different post ID, basename, folder, or extension.
func twitterFilenameSlot(relative string, posts []models.SourcePostIdentifier) (string, int, bool) {
	parts := twitterOriginalFilename.FindStringSubmatch(path.Base(relative))
	if parts == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(parts[2])
	if err != nil || n > archive.MaxManifestPositions {
		return "", 0, false
	}
	for _, post := range posts {
		if post.Namespace == "native:twitter" && post.Value == parts[1] {
			return parts[1] + "_" + parts[2], n - 1, true
		}
	}
	return "", 0, false
}

func twitterFilenameTitle(value string) bool {
	return value == "" || twitterOriginalFilename.MatchString(value) || twitterOriginalFilename.MatchString(value+".jpg")
}

func filenameAttachmentID(post string, ref models.SourcePostIdentifier) string {
	return uuid.NewSHA1(uuid.MustParse(post), []byte("source-filename-attachment-v1\x00"+ref.Namespace+"\x00"+ref.Value)).String()
}

func filenameSelection(selection *models.AttachmentSelection) bool {
	return selection != nil && len(selection.Entries) > 0 && slices.ContainsFunc(selection.Entries, func(e models.SourceAttachmentManifestEntry) bool {
		return e.Attachment.Reference.Namespace == twitterFilenameNamespace
	})
}

// Recover only missing source lists. Existing automatic, pinned and disabled
// decisions all win; retained captured lists require their normal source review.
// Source media IDs are stronger evidence and are never replaced by slot keys.
func (s *SourceGalleryStore) previewFilenameSelection(ctx context.Context, post string) (*models.AttachmentSelection, bool, error) {
	store := &SourceAttachmentStore{}
	current, err := store.Selection(ctx, post)
	if err != nil || current != nil {
		return nil, false, err
	}
	var captured bool
	if err := dbWrapper.Get(ctx, &captured, `SELECT EXISTS(SELECT 1 FROM source_post_identities i
 JOIN source_capture_attachment_manifests m ON m.post_uuid=i.post_uuid WHERE i.canonical_uuid=?)`, post); err != nil {
		return nil, false, err
	}
	if captured {
		return nil, true, nil
	}
	posts, err := sourceAlbumPostIdentifiers(ctx, post)
	if err != nil {
		return nil, false, err
	}
	rows, err := sourceAlbumEvidenceRows(ctx, post)
	if err != nil {
		return nil, false, err
	}
	entries := make(map[int]models.SourceAttachmentManifestEntry)
	postID := ""
	for _, row := range rows {
		if row.Basis != "legacy" || !row.PostFileUUID.Valid || !row.MatchUUID.Valid || !row.FileUUID.Valid {
			continue
		}
		key, position, ok := twitterFilenameSlot(row.RelativePath.String, posts)
		if !ok {
			continue
		}
		owner, _, _ := strings.Cut(key, "_")
		if postID != "" && postID != owner {
			return nil, true, nil // consolidated distinct tweets do not share slot numbers
		}
		postID = owner
		// Leave explicit IDs to the captured/source-ID path. An unrecognized
		// value cannot silently become a different, apparently valid identity.
		if row.SourceMediaType.Valid && row.SourceMediaType.String != "null" {
			return nil, true, nil
		}
		ref := models.SourcePostIdentifier{Namespace: twitterFilenameNamespace, Value: key}
		attachment, err := store.Lookup(ctx, post, ref)
		if err != nil {
			return nil, false, err
		}
		if attachment == nil {
			attachment = &models.SourceAttachment{UUID: filenameAttachmentID(post, ref), PostUUID: post, Reference: ref, Revision: 1}
		}
		// Extension is not a media-kind guarantee (for example GIF -> MP4).
		entries[position] = models.SourceAttachmentManifestEntry{Position: position, MediaKind: "unknown", Attachment: *attachment}
	}
	if len(entries) == 0 || (len(entries) == 1 && entries[0].Attachment.UUID != "") {
		return nil, false, nil // a lone _1 file is not evidence of an album
	}
	if len(entries) > archive.MaxManifestEntries {
		return nil, false, models.ErrSourceAlbumLimit
	}
	// This is only a metadata anchor, not the witness for the recovered list.
	// Read a bounded indexed page per identity member, preferring retained text.
	var captures []struct {
		UUID string         `db:"uuid"`
		Text sql.NullString `db:"text"`
	}
	if err := dbWrapper.Select(ctx, &captures, `WITH candidates AS MATERIALIZED (
 SELECT item.value AS uuid FROM source_post_identities i CROSS JOIN json_each((
  SELECT json_group_array(uuid) FROM (SELECT uuid FROM source_captures
   WHERE post_uuid=i.post_uuid ORDER BY captured_at DESC,uuid DESC LIMIT 64)
 )) item WHERE i.canonical_uuid=?
 ) SELECT c.uuid,json_extract(r.metadata,'$.original_text') AS text FROM candidates
 JOIN source_captures c ON c.uuid=candidates.uuid JOIN source_post_revisions r ON r.uuid=c.revision_uuid
 ORDER BY (coalesce(json_extract(r.metadata,'$.original_text'),'')=''),c.uuid LIMIT 1`, post); err != nil {
		return nil, false, err
	}
	if len(captures) == 0 {
		return nil, true, nil
	}
	ret := &models.AttachmentSelection{DeclaredAlbum: true, Decision: models.AttachmentSelectionDecision{
		PostUUID: post, Mode: "automatic", Origin: "migration", Reason: twitterFilenameReason, CaptureUUID: &captures[0].UUID,
		ManifestUUIDs: []string{},
	}}
	for _, entry := range entries {
		ret.Entries = append(ret.Entries, entry)
	}
	slices.SortFunc(ret.Entries, func(a, b models.SourceAttachmentManifestEntry) int { return a.Position - b.Position })
	return ret, false, nil
}

func (s *SourceGalleryStore) recoverFilenameSelection(ctx context.Context, post string, proposed *models.AttachmentSelection) error {
	store := &SourceAttachmentStore{}
	current, err := store.Selection(ctx, post)
	if err != nil {
		return err
	}
	if current != nil || !filenameSelection(proposed) {
		return models.ErrSourceGalleryConflict
	}
	input := models.SourceAttachmentManifestInput{DeclaredAlbum: true}
	for _, entry := range proposed.Entries {
		input.Entries = append(input.Entries, models.SourceAttachmentEntry{Position: entry.Position, Reference: entry.Attachment.Reference, MediaKind: entry.MediaKind})
	}
	manifest, err := store.recordManifestForPost(ctx, post, input, filenameAttachmentID)
	if err != nil {
		return err
	}
	proposed.Decision.ManifestUUIDs = []string{manifest.UUID}
	parent, err := currentSourcePost(ctx, post)
	if err != nil {
		return err
	}
	_, err = store.publishSelection(ctx, parent, models.AttachmentSelectionInput{PostUUID: post, ExpectedPostRevision: parent.Revision,
		Mode: "automatic", Origin: "migration", Reason: twitterFilenameReason}, proposed)
	return err
}
