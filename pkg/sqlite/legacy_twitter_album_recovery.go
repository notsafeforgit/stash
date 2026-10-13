package sqlite

// LEGACY-UPGRADE-ONLY: one-time repair for pre-native gallery-dl imports which lost the Twitter
// post ID or created one anonymous post per file. This is offline upgrade
// tooling, not a live ingest path. Remove it with the legacy import tooling.

import (
	"context"
	"encoding/json"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type LegacyTwitterFile struct {
	FileUUID        string `json:"file_uuid"`
	MediaUUID       string `json:"media_uuid"`
	Generation      int64  `json:"generation"`
	ObservationUUID string `json:"observation_uuid,omitempty"`
	MatchUUID       string `json:"match_uuid,omitempty"`
}

type LegacyTwitterRecovery struct {
	TweetID  string              `json:"tweet_id"`
	Evidence []LegacyTwitterFile `json:"evidence"`
}

type LegacyTwitterSlot struct {
	LegacyTwitterFile
	Position      int                      `json:"position"`
	SourcePath    string                   `json:"source_path"`
	LibraryPath   string                   `json:"library_path"`
	Size          int64                    `json:"size"`
	MediaKind     models.ArchiveEntityKind `json:"media_kind"`
	MediaRevision int                      `json:"media_revision"`
}

type LegacyTwitterRecoveryPreview struct {
	TweetID              string              `json:"tweet_id"`
	PostUUID             string              `json:"post_uuid,omitempty"`
	State                string              `json:"state"`
	Slots                []LegacyTwitterSlot `json:"slots"`
	Fragments            []models.SourcePost `json:"fragments"`
	Signature            string              `json:"signature"`
	ConflictingPositions []int               `json:"conflicting_positions,omitempty"`
}

func legacyTwitterPath(value, tweet string) (int, bool) {
	parts := twitterOriginalFilename.FindStringSubmatch(path.Base(value))
	if len(parts) == 0 || parts[1] != tweet || !strings.HasSuffix(path.Dir(value), ", twitter") {
		return 0, false
	}
	_, position, ok := twitterFilenameSlot(value, []models.SourcePostIdentifier{{Namespace: "native:twitter", Value: tweet}})
	return position, ok
}

func legacyTwitterFile(ctx context.Context, tweet string, input LegacyTwitterFile) (*LegacyTwitterSlot, error) {
	if sourceFileIDs(&input.FileUUID, &input.MediaUUID) != nil || input.Generation < 1 {
		return nil, models.ErrSourceFileEvidenceInvalid
	}
	entities := &ArchiveEntityStore{}
	file, err := entities.Find(ctx, input.FileUUID)
	if err != nil {
		return nil, err
	}
	media, err := entities.Find(ctx, input.MediaUUID)
	if err != nil {
		return nil, err
	}
	valid, err := fileBelongsToArchiveMedia(ctx, media, file)
	if err != nil || !valid {
		return nil, models.ErrSourceFileEvidenceInvalid
	}
	var row struct {
		Path       string `db:"path"`
		Size       int64  `db:"size"`
		Generation int64  `db:"generation"`
		Owners     int    `db:"owners"`
	}
	if err := dbWrapper.Get(ctx, &row, `SELECT d.path||'/'||f.basename AS path,f.size,f.generation,
 (SELECT count(*) FROM images_files WHERE file_id=f.id)+(SELECT count(*) FROM scenes_files WHERE file_id=f.id) AS owners
 FROM files f JOIN folders d ON d.id=f.parent_folder_id WHERE f.id=? AND f.zip_file_id IS NULL`, *file.LocalID); err != nil {
		return nil, err
	}
	if row.Generation != input.Generation || row.Owners != 1 {
		return nil, models.ErrSourceFileEvidenceInvalid
	}
	source := row.Path
	if input.ObservationUUID != "" || input.MatchUUID != "" {
		if sourceFileIDs(&input.ObservationUUID, &input.MatchUUID) != nil {
			return nil, models.ErrSourceFileEvidenceInvalid
		}
		if err := dbWrapper.Get(ctx, &source, `SELECT o.relative_path FROM source_file_matches m
 JOIN source_file_observations o ON o.uuid=m.observation_uuid
 WHERE m.uuid=? AND o.uuid=? AND m.file_uuid=? AND m.generation=?
 AND m.archive_file_uuid IS NULL AND o.archive_path IS NULL AND o.state IN ('present','deduplicated')`,
			input.MatchUUID, input.ObservationUUID, input.FileUUID, input.Generation); err != nil {
			return nil, err
		}
	}
	position, ok := legacyTwitterPath(source, tweet)
	if !ok {
		return nil, models.ErrSourceFileEvidenceInvalid
	}
	return &LegacyTwitterSlot{LegacyTwitterFile: input, Position: position, SourcePath: source,
		LibraryPath: row.Path, Size: row.Size, MediaKind: media.Kind, MediaRevision: media.Revision}, nil
}

// Only anonymous fragments whose complete original filename evidence belongs
// to this tweet may be consolidated. Sharing media with a different post is
// normal and never establishes that the posts themselves are the same.
func legacyTwitterFragments(ctx context.Context, tweet string, members map[string]bool) ([]models.SourcePost, error) {
	ids := make([]string, 0, len(members))
	for id := range members {
		ids = append(ids, id)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	var candidates []string
	if err := dbWrapper.Select(ctx, &candidates, `SELECT DISTINCT i.canonical_uuid FROM post_media_links l
 JOIN source_post_identities i ON i.post_uuid=l.post_uuid
 WHERE l.media_uuid IN (SELECT value FROM json_each(?)) ORDER BY i.canonical_uuid`, string(encoded)); err != nil {
		return nil, err
	}
	ret := []models.SourcePost{}
	for _, id := range candidates {
		keys, err := sourceAlbumPostIdentifiers(ctx, id)
		if err != nil {
			return nil, err
		}
		if len(keys) == 0 || slices.ContainsFunc(keys, func(k models.SourcePostIdentifier) bool { return !strings.HasPrefix(k.Namespace, "legacy:catalog:") }) {
			continue
		}
		var originals []string
		if err := dbWrapper.Select(ctx, &originals, `SELECT DISTINCT o.relative_path FROM source_post_identities i
 JOIN source_post_file_evidence e ON e.post_uuid=i.post_uuid JOIN source_file_observations o ON o.uuid=e.observation_uuid
 WHERE i.canonical_uuid=? LIMIT 8193`, id); err != nil {
			return nil, err
		}
		if len(originals) == 0 || len(originals) > 8192 || slices.ContainsFunc(originals, func(p string) bool { _, ok := legacyTwitterPath(p, tweet); return !ok }) {
			continue
		}
		post, err := currentSourcePost(ctx, id)
		if err != nil {
			return nil, err
		}
		var chosen bool
		if err := dbWrapper.Get(ctx, &chosen, `SELECT EXISTS(SELECT 1 FROM source_post_identities i
 WHERE i.canonical_uuid=? AND (EXISTS(SELECT 1 FROM post_attachment_selections s WHERE s.post_uuid=i.post_uuid)
 OR EXISTS(SELECT 1 FROM post_gallery_links g WHERE g.post_uuid=i.post_uuid)))`, id); err != nil {
			return nil, err
		}
		states, err := sourcePostMediaStates(ctx, id)
		if err != nil {
			return nil, err
		}
		if post == nil || post.State != "active" || chosen {
			return nil, models.ErrSourceGalleryConflict
		}
		for media, state := range states {
			if !members[media] || state != "linked" {
				return nil, models.ErrSourceGalleryConflict
			}
		}
		ret = append(ret, *post)
	}
	return ret, nil
}

func previewLegacyTwitterRecovery(ctx context.Context, input LegacyTwitterRecovery) (*LegacyTwitterRecoveryPreview, error) {
	if !sourceAlbumASCIIID(input.TweetID, false) || len(input.TweetID) < 15 || len(input.Evidence) < 1 || len(input.Evidence) > 8192 {
		return nil, models.ErrSourceFileEvidenceInvalid
	}
	post, err := (&SourceEvidenceStore{}).FindPostByIdentifier(ctx, models.SourcePostIdentifier{Namespace: "native:twitter", Value: input.TweetID})
	if err != nil {
		return nil, err
	}
	ret := &LegacyTwitterRecoveryPreview{TweetID: input.TweetID, State: "ready", Slots: []LegacyTwitterSlot{}, Fragments: []models.SourcePost{}}
	if post != nil {
		ret.State, ret.PostUUID = "existing", post.UUID
		return ret, nil
	}
	positions, members := make(map[int]map[string]bool), make(map[string]bool)
	for _, file := range input.Evidence {
		slot, err := legacyTwitterFile(ctx, input.TweetID, file)
		if err != nil {
			return nil, err
		}
		if positions[slot.Position][slot.MediaUUID] {
			continue
		}
		if positions[slot.Position] == nil {
			positions[slot.Position] = make(map[string]bool)
		}
		positions[slot.Position][slot.MediaUUID], members[slot.MediaUUID] = true, true
		ret.Slots = append(ret.Slots, *slot)
	}
	if len(ret.Slots) == 1 && ret.Slots[0].Position == 0 {
		return nil, models.ErrSourceGalleryConflict
	}
	for position, media := range positions {
		if len(media) > 1 {
			ret.ConflictingPositions = append(ret.ConflictingPositions, position)
		}
	}
	slices.Sort(ret.ConflictingPositions)
	slices.SortFunc(ret.Slots, func(a, b LegacyTwitterSlot) int {
		if a.Position == b.Position {
			return strings.Compare(a.MediaUUID, b.MediaUUID)
		}
		return a.Position - b.Position
	})
	ret.Fragments, err = legacyTwitterFragments(ctx, input.TweetID, members)
	if err != nil {
		return nil, err
	}
	ret.Signature, err = sourceSignature("legacy-twitter-library-recovery-v1", ret)
	return ret, err
}

func (db *Database) PreviewLegacyTwitterRecovery(ctx context.Context, input LegacyTwitterRecovery) (*LegacyTwitterRecoveryPreview, error) {
	var ret *LegacyTwitterRecoveryPreview
	repo := db.Repository()
	err := repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		ret, err = previewLegacyTwitterRecovery(ctx, input)
		return err
	})
	return ret, err
}

func (db *Database) ApplyLegacyTwitterRecovery(ctx context.Context, input LegacyTwitterRecovery, signature string) (string, error) {
	var postID string
	repo := db.Repository()
	err := repo.WithTxn(ctx, func(ctx context.Context) error {
		preview, err := previewLegacyTwitterRecovery(ctx, input)
		if err != nil {
			return err
		}
		if preview.State == "existing" {
			postID = preview.PostUUID
			return nil
		}
		if signature == "" || signature != preview.Signature {
			return models.ErrSourceGalleryConflict
		}
		postID, err = applyLegacyTwitterRecovery(ctx, preview)
		return err
	})
	return postID, err
}

func applyLegacyTwitterRecovery(ctx context.Context, preview *LegacyTwitterRecoveryPreview) (string, error) {
	store, attachments := &SourceEvidenceStore{}, &SourceAttachmentStore{}
	reference := models.SourcePostIdentifier{Namespace: "native:twitter", Value: preview.TweetID}
	reason := "Recover Twitter post and attachment order from original gallery-dl filenames"
	var post *models.SourcePost
	var err error
	if len(preview.Fragments) == 0 {
		post, err = store.EnsurePost(ctx, reference, "")
	} else {
		post = &preview.Fragments[0]
		err = store.AddPostIdentifier(ctx, post.UUID, reference, post.Revision)
	}
	if err != nil {
		return "", err
	}
	for index, fragment := range preview.Fragments {
		if index == 0 {
			continue
		}
		input := models.PostConsolidationReviewInput{SourceUUID: fragment.UUID, DestinationUUID: post.UUID, Reason: reason}
		plan, err := store.PreviewConsolidationReview(ctx, input)
		if err != nil {
			return "", err
		}
		if !plan.Ready {
			return "", models.ErrSourceGalleryConflict
		}
		if _, _, err := store.ApplyConsolidationReview(ctx, models.PostConsolidationReviewApplyInput{PostConsolidationReviewInput: input, RequestUUID: uuid.NewString(), Digest: plan.Digest}, time.Now().UTC()); err != nil {
			return "", err
		}
	}
	capture, err := sourceAlbumMetadataAnchor(ctx, post.UUID)
	if err != nil {
		return "", err
	}
	if capture == "" {
		// There was no saved source text. Record only a local filename recovery,
		// with an unknown original observation date, never a fictitious scrape.
		payload, err := archive.PrepareRetainedCapture("filename-recovery", "twitter", json.RawMessage(`{}`))
		if err != nil {
			return "", err
		}
		now := time.Now().UTC()
		recorded, err := store.RecordCapture(ctx, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID,
			Origin: "filename-recovery", Platform: "twitter", RecordedAt: &now, RetentionPolicy: archive.SourceRetentionVersion,
			Metadata: models.SourcePostMetadata{Title: &preview.TweetID}, Payload: *payload})
		if err != nil {
			return "", err
		}
		capture = recorded.UUID
	}
	selection := &models.AttachmentSelection{DeclaredAlbum: true, Decision: models.AttachmentSelectionDecision{
		PostUUID: post.UUID, Mode: "automatic", Origin: "migration", Reason: twitterFilenameReason, CaptureUUID: &capture}}
	for _, slot := range preview.Slots {
		if slices.Contains(preview.ConflictingPositions, slot.Position) {
			continue // The post ID is certain; the repeated ordinal is not.
		}
		ref := models.SourcePostIdentifier{Namespace: twitterFilenameNamespace, Value: path.Base(slot.SourcePath)}
		ref.Value = strings.TrimSuffix(ref.Value, path.Ext(ref.Value))
		kind := "image"
		if slot.MediaKind == models.ArchiveScene {
			kind = "video"
		}
		selection.Entries = append(selection.Entries, models.SourceAttachmentManifestEntry{Position: slot.Position, MediaKind: kind,
			Attachment: models.SourceAttachment{UUID: filenameAttachmentID(post.UUID, ref), PostUUID: post.UUID, Reference: ref, Revision: 1}})
	}
	if len(selection.Entries) == 0 {
		selection.Decision.Reason = linkedMediaAlbumReason
	}
	if err := (&SourceGalleryStore{}).recoverAlbumSelection(ctx, post.UUID, selection); err != nil {
		return "", err
	}
	attachmentIDs := make(map[int]string, len(selection.Entries))
	for _, entry := range selection.Entries {
		attachmentIDs[entry.Position] = entry.Attachment.UUID
	}
	for _, slot := range preview.Slots {
		attachmentID := attachmentIDs[slot.Position]
		details, err := json.Marshal(slot)
		if err != nil {
			return "", err
		}
		if _, err := attachments.RecordMediaEvidence(ctx, models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: post.UUID,
			AttachmentUUID: attachmentID, MediaUUID: slot.MediaUUID, FileUUID: &slot.FileUUID, Basis: "legacy", Details: details}); err != nil {
			return "", err
		}
		if attachmentID == "" {
			if err := linkLegacyTwitterMember(ctx, post.UUID, slot.MediaUUID, reason); err != nil {
				return "", err
			}
			continue
		}
		current, err := attachments.Find(ctx, attachmentID)
		if err != nil {
			return "", err
		}
		if _, err := attachments.DecideMedia(ctx, models.AttachmentMediaDecisionInput{AttachmentUUID: current.UUID,
			ExpectedAttachmentRevision: current.Revision, State: "linked", MediaUUID: slot.MediaUUID,
			ExpectedMediaRevision: slot.MediaRevision, Origin: "migration", Reason: reason}); err != nil {
			return "", err
		}
	}
	return post.UUID, nil
}

func linkLegacyTwitterMember(ctx context.Context, post, media, reason string) error {
	store := &SourcePostMediaStore{}
	association, err := store.Association(ctx, post, media)
	if err != nil {
		return err
	}
	if association.State == "linked" {
		return nil
	}
	if association.State != "undecided" || len(association.Decisions) != 0 {
		return models.ErrSourceGalleryConflict
	}
	_, err = store.decide(ctx, models.SourcePostMediaInput{UUID: uuid.NewString(), PostUUID: post, MediaUUID: media,
		ExpectedPostRevision: association.PostRevision, ExpectedMediaRevision: association.MediaRevision,
		State: "linked", Origin: "migration", Reason: reason}, false)
	return err
}
