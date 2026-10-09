package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

const nfoSourceTables = "('sidecars','sidecar_documents','sidecar_sources','sidecar_heads')"

type NFOCleanupResult struct {
	Captures, Documents, RecoveredDocuments, RecoveredMedia, MissingMedia, StagingRows, Catalogs int
}

// CompactImportedNFO is an explicit, offline maintenance operation. All changes,
// including temporary relaxation of immutable-input guards, commit together.
// Completed catalog receipts remain aggregate receipts; no per-NFO retirement
// records, paths or document checksums are retained by this operation.
func (db *Database) CompactImportedNFO(ctx context.Context, progress func(string, int)) (*NFOCleanupResult, error) {
	r := &NFOCleanupResult{}
	err := txn.WithTxn(ctx, db, func(ctx context.Context) error {
		for _, query := range []string{
			`SELECT EXISTS(SELECT 1 FROM catalog_snapshots s WHERE s.nfo_compacted=0 AND (s.state!='received' OR NOT EXISTS(SELECT 1 FROM catalog_document_imports i WHERE i.snapshot_uuid=s.uuid AND i.state='mapped' AND i.phase='complete')))`,
			`SELECT EXISTS(SELECT 1 FROM source_documents WHERE parser!='legacy-catalog-nfo-v1')`,
			`SELECT EXISTS(SELECT 1 FROM source_document_sources s JOIN source_documents d ON d.uuid=s.document_uuid WHERE d.parse_status IN ('valid','repaired') AND s.post_uuid IS NULL AND NOT EXISTS(SELECT 1 FROM metadata_policy_import_documents p WHERE p.source_uuid=s.uuid))`,
		} {
			var unsafe bool
			if err := dbWrapper.Get(ctx, &unsafe, query); err != nil {
				return err
			}
			if unsafe {
				return errors.New("NFO cleanup requires completed imports and accounted-for document sources")
			}
		}
		var guards []struct {
			Name string `db:"name"`
			SQL  string `db:"sql"`
		}
		if err := dbWrapper.Select(ctx, &guards, `SELECT name,sql FROM sqlite_schema WHERE type='trigger' AND name IN ('source_capture_immutable','catalog_snapshot_record_immutable','catalog_evidence_record_immutable')`); err != nil {
			return err
		}
		if len(guards) != 3 {
			return errors.New("NFO cleanup immutable guards are missing")
		}
		for _, guard := range guards {
			if _, err := dbWrapper.Exec(ctx, "DROP TRIGGER "+guard.Name); err != nil {
				return err
			}
		}
		// These work tables exist only inside this transaction, never as durable
		// document replacements. They bound payload garbage collection and ensure
		// each retained document value is represented by ordinary post metadata.
		for _, query := range []string{
			`CREATE TEMP TABLE nfo_old_revisions(uuid TEXT PRIMARY KEY)`,
			`CREATE TEMP TABLE nfo_old_payloads(digest TEXT PRIMARY KEY)`,
			`CREATE TEMP TABLE nfo_domain_values(post_uuid TEXT, value TEXT, PRIMARY KEY(post_uuid,value)) WITHOUT ROWID`,
			`CREATE TEMP TABLE nfo_policy_imports AS SELECT DISTINCT import_uuid FROM metadata_policy_import_documents`,
		} {
			if _, err := dbWrapper.Exec(ctx, query); err != nil {
				return err
			}
		}
		if err := db.compactNFOCaptures(ctx, r, progress); err != nil {
			return err
		}
		if err := db.collateNFODocuments(ctx, r, progress); err != nil {
			return err
		}
		if err := compactNFOStaging(ctx, r, progress); err != nil {
			return err
		}
		// Historical enrichment receipts reference alias rows in this same
		// staging table. Without this lookup SQLite scans every enrichment row
		// for each discarded document row while checking its foreign keys.
		// Keep the maintenance index only for the duration of this transaction.
		if _, err := dbWrapper.Exec(ctx, `CREATE INDEX nfo_cleanup_enrichment_alias ON automation_enrichment_records(catalog_snapshot_uuid,alias_ordinal)`); err != nil {
			return err
		}
		for step, query := range []string{
			`DELETE FROM metadata_policy_import_documents`,
			`DELETE FROM metadata_policy_imports WHERE uuid IN (SELECT import_uuid FROM nfo_policy_imports)`,
			`DELETE FROM catalog_document_records`,
			`DELETE FROM source_document_heads`,
			`DELETE FROM source_document_head_decisions`,
			`DELETE FROM source_document_head_claims`,
			`DELETE FROM source_document_sources`,
			`DELETE FROM source_documents`,
			`DELETE FROM source_document_contents`,
			`DELETE FROM catalog_snapshot_records WHERE source_table IN ` + nfoSourceTables,
			`UPDATE catalog_snapshots SET last_table='',last_key='[]' WHERE last_table IN ` + nfoSourceTables,
			`DELETE FROM source_post_revisions WHERE uuid IN (SELECT uuid FROM nfo_old_revisions) AND NOT EXISTS(SELECT 1 FROM source_captures c WHERE c.revision_uuid=source_post_revisions.uuid)`,
			`DELETE FROM source_payloads WHERE digest IN (SELECT digest FROM nfo_old_payloads) AND NOT EXISTS(SELECT 1 FROM source_captures c WHERE c.patch_digest=source_payloads.digest) AND NOT EXISTS(SELECT 1 FROM source_post_revisions r WHERE r.body_digest=source_payloads.digest) AND NOT EXISTS(SELECT 1 FROM source_profile_bodies p WHERE p.payload_digest=source_payloads.digest)`,
		} {
			if progress != nil {
				progress("discarding", step)
			}
			if _, err := dbWrapper.Exec(ctx, query); err != nil {
				return err
			}
		}
		if _, err := dbWrapper.Exec(ctx, `DROP INDEX nfo_cleanup_enrichment_alias`); err != nil {
			return err
		}
		result, err := dbWrapper.Exec(ctx, `UPDATE catalog_snapshots SET nfo_compacted=1 WHERE nfo_compacted=0`)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		r.Catalogs = int(n)
		for _, guard := range guards {
			if _, err := dbWrapper.Exec(ctx, guard.SQL); err != nil {
				return err
			}
		}
		for _, table := range []string{"nfo_old_revisions", "nfo_old_payloads", "nfo_domain_values", "nfo_policy_imports"} {
			if _, err := dbWrapper.Exec(ctx, "DROP TABLE temp."+table); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

func nfoDomainValue(data *archive.LegacyNFOPostData) (string, error) {
	// This transient fingerprint includes the values themselves, never a hash
	// of an original document or path. It is discarded with the transaction.
	body, err := json.Marshal(data)
	return string(body), err
}

func nfoDomainPayload(data *archive.LegacyNFOPostData) (json.RawMessage, error) {
	payload := map[string]any{}
	if len(data.Performers) > 0 {
		payload["performers"] = data.Performers
	}
	if data.Studio != nil {
		payload["studio"] = *data.Studio
	}
	return archive.EncodeSourceJSON(payload)
}

func (db *Database) retainNFODomainLinks(ctx context.Context, post string, data *archive.LegacyNFOPostData) error {
	for _, address := range data.URLs {
		var exists bool
		if err := dbWrapper.Get(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM source_post_urls WHERE post_uuid=? AND url=?)`, post, address); err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := (&SourcePostLinksStore{}).ObserveURL(ctx, models.SourcePostURLInput{SourcePostEvidence: models.SourcePostEvidence{
			UUID: uuid.NewString(), PostUUID: post, Origin: "migration", Basis: "imported-metadata", ObservedAt: time.Now().UTC(), Details: json.RawMessage(`{}`)}, URL: address}); err != nil {
			return err
		}
	}
	for _, input := range data.Translations {
		translation, err := (&SourceTranslationStore{}).Retain(ctx, input)
		if err != nil {
			return err
		}
		var exists bool
		if err := dbWrapper.Get(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM source_translation_evidence WHERE post_uuid=? AND translation_uuid=?)`, post, translation.UUID); err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := (&SourceTranslationStore{}).RecordEvidence(ctx, models.SourceTranslationEvidence{UUID: uuid.NewString(), TranslationUUID: translation.UUID, PostUUID: post, Provenance: "imported-metadata", Origin: "migration", Details: json.RawMessage(`{}`)}); err != nil {
			return err
		}
	}
	value, err := nfoDomainValue(data)
	if err != nil {
		return err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT OR IGNORE INTO nfo_domain_values(post_uuid,value) VALUES(?,?)`, post, value)
	return err
}

func (db *Database) compactNFOCaptures(ctx context.Context, r *NFOCleanupResult, progress func(string, int)) error {
	evidence := &SourceEvidenceStore{gallery: db.Gallery}
	after := ""
	for {
		var ids []string
		if err := dbWrapper.Select(ctx, &ids, `SELECT uuid FROM source_captures WHERE uuid>? AND origin='legacy-nfo' AND retention_policy=? ORDER BY uuid LIMIT 1000`, after, legacyRetentionPolicy); err != nil {
			return err
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			capture, err := evidence.FindCapture(ctx, id)
			if err != nil {
				return err
			}
			if len(capture.Payload.Refs) != 0 || len(capture.Contexts) != 0 {
				return errors.New("unexpected NFO profile or context references")
			}
			body, err := archive.RestoreCapture(capture.Payload)
			if err != nil {
				return err
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(body, &object); err != nil {
				return err
			}
			for key := range object {
				if key != "nfo_fields" && key != "nfo_path" {
					return fmt.Errorf("unmapped NFO capture field %q", key)
				}
			}
			data, err := archive.CollateLegacyNFOFields(object["nfo_fields"])
			if err != nil {
				return err
			}
			// Source language can be independent of a translation's target language.
			metadata := data.Metadata
			metadata.Language = capture.Metadata.Language
			// The old catalog normalizer represented empty scalar strings as
			// absent metadata. They carry the same value; keep that representation.
			for _, field := range []struct {
				projected **string
				retained  *string
			}{
				{&metadata.Title, capture.Metadata.Title}, {&metadata.OriginalText, capture.Metadata.OriginalText},
				{&metadata.PublishedAt, capture.Metadata.PublishedAt},
			} {
				if *field.projected != nil && **field.projected == "" && field.retained == nil {
					*field.projected = nil
				}
			}
			if metadata.PublishedAt == nil {
				metadata.DateBasis = capture.Metadata.DateBasis
			}
			if !reflect.DeepEqual(metadata, capture.Metadata) {
				return fmt.Errorf("NFO capture %s differs from its projected metadata", id)
			}
			payload, err := nfoDomainPayload(data)
			if err != nil {
				return err
			}
			input := models.SourceCaptureInput{UUID: id, PostUUID: capture.PostUUID, Origin: capture.Origin, Platform: capture.Platform, CapturedAt: capture.CapturedAt, RecordedAt: capture.RecordedAt, ExtractorVersion: capture.ExtractorVersion, RetentionPolicy: importedMetadataPolicy, Metadata: metadata, Payload: models.SourceCapturePayload{Shared: payload, Patch: json.RawMessage(`{}`)}}
			metadataJSON, signature, err := canonicalCaptureInput(&input)
			if err != nil {
				return err
			}
			bodyDigest, err := putSourcePayload(ctx, input.Payload.Shared)
			if err != nil {
				return err
			}
			patchDigest, err := putSourcePayload(ctx, input.Payload.Patch)
			if err != nil {
				return err
			}
			captureSig, err := captureSignature(input, signature, patchDigest, input.Payload.Refs)
			if err != nil {
				return err
			}
			if _, err := dbWrapper.Exec(ctx, `INSERT OR IGNORE INTO nfo_old_revisions VALUES(?)`, capture.RevisionUUID); err != nil {
				return err
			}
			for _, old := range []json.RawMessage{capture.Payload.Shared, capture.Payload.Patch} {
				if _, err := dbWrapper.Exec(ctx, `INSERT OR IGNORE INTO nfo_old_payloads VALUES(?)`, sourceDigest(old)); err != nil {
					return err
				}
			}
			if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_post_revisions(uuid,post_uuid,signature,body_digest,metadata,structure_version) VALUES(?,?,?,?,?,?) ON CONFLICT(post_uuid,signature) DO NOTHING`, uuid.NewString(), capture.PostUUID, signature, bodyDigest, metadataJSON, archive.CaptureStructureVersion); err != nil {
				return err
			}
			var revision string
			if err := dbWrapper.Get(ctx, &revision, `SELECT uuid FROM source_post_revisions WHERE post_uuid=? AND signature=?`, capture.PostUUID, signature); err != nil {
				return err
			}
			if _, err := dbWrapper.Exec(ctx, `UPDATE source_captures SET revision_uuid=?,retention_policy=?,patch_digest=?,signature=? WHERE uuid=?`, revision, importedMetadataPolicy, patchDigest, captureSig, id); err != nil {
				return err
			}
			if err := db.retainNFODomainLinks(ctx, capture.PostUUID, data); err != nil {
				return err
			}
			r.Captures++
		}
		after = ids[len(ids)-1]
		if progress != nil && r.Captures%10000 == 0 {
			progress("captures", r.Captures)
		}
	}
	_, err := dbWrapper.Exec(ctx, `UPDATE source_posts SET revision=revision+1 WHERE uuid IN (SELECT DISTINCT post_uuid FROM nfo_domain_values)`)
	return err
}

func recoveredNFOPostKey(address string) (models.SourcePostIdentifier, string, error) {
	u, err := url.Parse(address)
	if err != nil {
		return models.SourcePostIdentifier{}, "", err
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if (u.Hostname() == "coomer.st" || u.Hostname() == "kemono.cr" || u.Hostname() == "kemono.su") && len(parts) == 5 && parts[1] == "user" && parts[3] == "post" && parts[0] != "" && parts[2] != "" && parts[4] != "" {
		platform := strings.Split(u.Hostname(), ".")[0]
		return models.SourcePostIdentifier{Namespace: "mirror:" + platform + ":" + parts[0], Value: parts[2] + "/" + parts[4]}, platform, nil
	}
	if (u.Hostname() == "x.com" || u.Hostname() == "twitter.com" || u.Hostname() == "www.twitter.com") && len(parts) == 3 && parts[1] == "status" && parts[2] != "" {
		for _, c := range parts[2] {
			if c < '0' || c > '9' {
				return models.SourcePostIdentifier{}, "", errors.New("invalid recovered post identifier")
			}
		}
		return models.SourcePostIdentifier{Namespace: "native:twitter", Value: parts[2]}, "twitter", nil
	}
	return models.SourcePostIdentifier{}, "", errors.New("unrecognized recovered post URL")
}

func (db *Database) collateNFODocuments(ctx context.Context, r *NFOCleanupResult, progress func(string, int)) error {
	after := ""
	for {
		var docs []struct {
			UUID   string `db:"uuid"`
			Status string `db:"parse_status"`
			Parsed string `db:"parsed"`
			Hash   string `db:"content_sha256"`
		}
		if err := dbWrapper.Select(ctx, &docs, `SELECT uuid,parse_status,parsed,content_sha256 FROM source_documents WHERE uuid>? ORDER BY uuid LIMIT 1000`, after); err != nil {
			return err
		}
		if len(docs) == 0 {
			break
		}
		for _, doc := range docs {
			r.Documents++
			parsed := json.RawMessage(doc.Parsed)
			switch doc.Status {
			case "empty":
				raw, err := (&SourceDocumentStore{}).Content(ctx, doc.Hash)
				if err != nil {
					return err
				}
				if len(strings.TrimSpace(string(raw))) != 0 {
					return errors.New("nonempty document marked empty")
				}
				continue
			case "invalid":
				raw, err := (&SourceDocumentStore{}).Content(ctx, doc.Hash)
				if err != nil {
					return err
				}
				parsed, err = archive.RecoverGeneratedNFOFields(raw)
				if err != nil {
					return fmt.Errorf("document %s: %w", doc.UUID, err)
				}
			case "valid", "repaired":
			default:
				return errors.New("unknown document parse status")
			}
			data, err := archive.CollateLegacyNFOFields(parsed)
			if err != nil {
				return err
			}
			var sources []struct {
				UUID       string  `db:"uuid"`
				Post       *string `db:"post_uuid"`
				Collection string  `db:"collection_uuid"`
				Revision   int     `db:"collection_revision"`
				Path       string  `db:"relative_path"`
			}
			if err := dbWrapper.Select(ctx, &sources, `SELECT uuid,post_uuid,collection_uuid,collection_revision,relative_path FROM source_document_sources WHERE document_uuid=?`, doc.UUID); err != nil {
				return err
			}
			if len(sources) == 0 {
				return errors.New("unassociated NFO document needs review")
			}
			for _, source := range sources {
				if doc.Status != "invalid" {
					if source.Post == nil {
						continue
					} // Guarded native folder policy import.
					value, err := nfoDomainValue(data)
					if err != nil {
						return err
					}
					var exists bool
					if err := dbWrapper.Get(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM nfo_domain_values WHERE post_uuid=? AND value=?)`, *source.Post, value); err != nil {
						return err
					}
					if !exists {
						return fmt.Errorf("document %s has values absent from its native post", doc.UUID)
					}
					continue
				}
				if len(data.URLs) != 1 {
					return errors.New("recovered NFO requires one qualified post URL")
				}
				key, platform, err := recoveredNFOPostKey(data.URLs[0])
				if err != nil {
					return err
				}
				evidence := &SourceEvidenceStore{gallery: db.Gallery}
				post, err := evidence.EnsurePost(ctx, key, "")
				if err != nil {
					return err
				}
				payload, err := nfoDomainPayload(data)
				if err != nil {
					return err
				}
				value, err := nfoDomainValue(data)
				if err != nil {
					return err
				}
				captureID := uuid.NewSHA1(uuid.MustParse(post.UUID), []byte("imported-metadata:"+value)).String()
				capture, err := evidence.FindCapture(ctx, captureID)
				if err != nil {
					return err
				}
				if capture == nil {
					now := time.Now().UTC()
					_, err = evidence.RecordCapture(ctx, models.SourceCaptureInput{UUID: captureID, PostUUID: post.UUID, Origin: "imported-metadata", Platform: platform, RecordedAt: &now, RetentionPolicy: importedMetadataPolicy, Metadata: data.Metadata, Payload: models.SourceCapturePayload{Shared: payload, Patch: json.RawMessage(`{}`)}})
					if err != nil {
						return err
					}
				}
				if err := db.retainNFODomainLinks(ctx, post.UUID, data); err != nil {
					return err
				}
				if err := (&SourceCollectionStore{}).RecordCapture(ctx, models.CollectionCapture{CaptureUUID: captureID, CollectionUUID: source.Collection, CollectionRevision: source.Revision}); err != nil {
					return err
				}
				if !strings.HasSuffix(source.Path, ".nfo") {
					return errors.New("unexpected document suffix")
				}
				stem := strings.TrimSuffix(source.Path, ".nfo")
				var observations []string
				if err := dbWrapper.Select(ctx, &observations, `SELECT uuid FROM source_file_observations WHERE collection_uuid=? AND relative_path>=? AND relative_path<? AND archive_path IS NULL`, source.Collection, stem+".", stem+"/"); err != nil {
					return err
				}
				if len(observations) == 0 {
					r.MissingMedia++
					continue
				}
				if len(observations) != 1 {
					return errors.New("ambiguous recovered post media location")
				}
				if err := db.linkRecoveredNFOMedia(ctx, post.UUID, captureID, observations[0], r); err != nil {
					return err
				}
			}
			if doc.Status == "invalid" {
				r.RecoveredDocuments++
			}
		}
		after = docs[len(docs)-1].UUID
		if progress != nil && r.Documents%10000 == 0 {
			progress("documents", r.Documents)
		}
	}
	return nil
}

func (db *Database) linkRecoveredNFOMedia(ctx context.Context, post, capture, observation string, r *NFOCleanupResult) error {
	fileStore := &SourceFileStore{}
	var conflicting bool
	if err := dbWrapper.Get(ctx, &conflicting, `SELECT EXISTS(SELECT 1 FROM source_post_file_evidence WHERE observation_uuid=? AND post_uuid!=?)`, observation, post); err != nil {
		return err
	}
	if conflicting {
		return errors.New("recovered media already belongs to another post; review required")
	}
	if _, err := fileStore.RecordPostEvidence(ctx, models.SourcePostFileEvidence{SourcePostEvidence: models.SourcePostEvidence{UUID: uuid.NewSHA1(uuid.MustParse(post), []byte("file:"+observation)).String(), PostUUID: post, Origin: "migration", Basis: "imported-metadata", ObservedAt: time.Now().UTC(), Details: json.RawMessage(`{}`)}, ObservationUUID: observation}); err != nil {
		return err
	}
	matches, err := fileStore.Matches(ctx, observation, "", 2)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		r.MissingMedia++
		return nil
	}
	if len(matches) != 1 {
		return errors.New("ambiguous recovered media file match")
	}
	if err := fileStore.validateMatch(ctx, &matches[0]); err != nil {
		return fmt.Errorf("recovered media match is no longer current: %w", err)
	}
	owners, err := (&FileContentStore{}).Owners(ctx, matches[0].FileUUID)
	if err != nil {
		return err
	}
	if len(owners) != 1 {
		return errors.New("ambiguous recovered media owner")
	}
	owner := owners[0]
	media := &SourcePostMediaStore{gallery: db.Gallery}
	association, err := media.Association(ctx, post, owner.UUID)
	if err != nil {
		return err
	}
	if association.Suppressed() || len(association.Decisions) > 0 {
		return nil
	}
	if _, err := (&SourceAttachmentStore{}).RecordMediaEvidence(ctx, models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: post, CaptureUUID: capture, MediaUUID: owner.UUID, FileUUID: &matches[0].FileUUID, Basis: "legacy", Details: json.RawMessage(`{}`)}); err != nil {
		return err
	}
	association, err = media.Association(ctx, post, owner.UUID)
	if err != nil {
		return err
	}
	if _, err := media.Decide(ctx, models.SourcePostMediaInput{UUID: uuid.NewString(), PostUUID: post, MediaUUID: owner.UUID, ExpectedPostRevision: association.PostRevision, ExpectedMediaRevision: association.MediaRevision, State: "linked", Origin: "migration", Reason: "Imported post metadata matched to its media file"}); err != nil {
		return err
	}
	r.RecoveredMedia++
	return nil
}

func compactNFOStaging(ctx context.Context, r *NFOCleanupResult, progress func(string, int)) error {
	var snapshots []string
	if err := dbWrapper.Select(ctx, &snapshots, `SELECT uuid FROM catalog_snapshots WHERE nfo_compacted=0 ORDER BY uuid`); err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		ordinal := 0
		for {
			var rows []struct {
				Ordinal int    `db:"ordinal"`
				Table   string `db:"source_table"`
				Data    string `db:"data"`
			}
			if err := dbWrapper.Select(ctx, &rows, `SELECT ordinal,source_table,data FROM catalog_snapshot_records WHERE snapshot_uuid=? AND ordinal>? AND source_table IN ('observations','observation_details') ORDER BY ordinal LIMIT 1000`, snapshot, ordinal); err != nil {
				return err
			}
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				var record map[string]json.RawMessage
				if err := json.Unmarshal([]byte(row.Data), &record); err != nil {
					return err
				}
				var values map[string]json.RawMessage
				if err := json.Unmarshal(record["values"], &values); err != nil {
					return err
				}
				field := "payload_json"
				if row.Table == "observation_details" {
					field = "payload_patch"
				}
				var encoded string
				if err := json.Unmarshal(values[field], &encoded); err != nil {
					return err
				}
				var payload map[string]json.RawMessage
				if err := json.Unmarshal([]byte(encoded), &payload); err != nil {
					return err
				}
				_, fields := payload["nfo_fields"]
				_, path := payload["nfo_path"]
				if !fields && !path {
					continue
				}
				// Values have been collated and checked above. Staging retains only
				// ordinary post columns, not duplicate parsed documents or paths.
				delete(payload, "nfo_fields")
				delete(payload, "nfo_path")
				body, err := json.Marshal(payload)
				if err != nil {
					return err
				}
				values[field], err = json.Marshal(string(body))
				if err != nil {
					return err
				}
				record["values"], err = json.Marshal(values)
				if err != nil {
					return err
				}
				body, err = json.Marshal(record)
				if err != nil {
					return err
				}
				digest := sourceDigest(body)
				if _, err := dbWrapper.Exec(ctx, `UPDATE catalog_snapshot_records SET data=?,byte_count=?,data_sha256=? WHERE snapshot_uuid=? AND ordinal=?`, string(body), len(body), digest, snapshot, row.Ordinal); err != nil {
					return err
				}
				if _, err := dbWrapper.Exec(ctx, `UPDATE catalog_evidence_records SET data_sha256=? WHERE snapshot_uuid=? AND ordinal=?`, digest, snapshot, row.Ordinal); err != nil {
					return err
				}
				r.StagingRows++
			}
			ordinal = rows[len(rows)-1].Ordinal
		}
		if progress != nil {
			progress("staging", r.StagingRows)
		}
	}
	return nil
}
