package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type MetadataPolicyImportStore struct{}

func (s *MetadataPolicyImportStore) Find(ctx context.Context, id string) (*models.MetadataPolicyImportDetails, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrMetadataPolicyImportInvalid
	}
	var row struct {
		Plan     string `db:"plan"`
		Binding  string `db:"binding"`
		Revision int    `db:"policy_revision"`
		Created  string `db:"created_at"`
	}
	err := dbWrapper.Get(ctx, &row, `SELECT plan,binding,policy_revision,created_at FROM metadata_policy_imports WHERE uuid=?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := &models.MetadataPolicyImportDetails{}
	if err := json.Unmarshal([]byte(row.Plan), &result.MetadataPolicyImportPlan); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(row.Binding), &result.Binding); err != nil {
		return nil, err
	}
	result.PolicyRevision = row.Revision
	result.CreatedAt, err = time.Parse(time.RFC3339Nano, row.Created)
	return result, err
}

func (s *MetadataPolicyImportStore) List(ctx context.Context, collection, after string, limit int) ([]models.MetadataPolicyImport, error) {
	if !validSourceRunUUID(collection) || (after != "" && !validSourceRunUUID(after)) {
		return nil, models.ErrMetadataPolicyImportInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrMetadataPolicyImportInvalid
	}
	var rows []struct {
		Plan     string `db:"plan"`
		Revision int    `db:"policy_revision"`
		Created  string `db:"created_at"`
	}
	if err := dbWrapper.Select(ctx, &rows, `SELECT plan,policy_revision,created_at FROM metadata_policy_imports
WHERE collection_uuid=? AND uuid>? ORDER BY uuid LIMIT ?`, collection, after, limit); err != nil {
		return nil, err
	}
	result := make([]models.MetadataPolicyImport, 0, len(rows))
	for _, row := range rows {
		item := models.MetadataPolicyImport{PolicyRevision: row.Revision}
		if err := json.Unmarshal([]byte(row.Plan), &item.MetadataPolicyImportPlan); err != nil {
			return nil, err
		}
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, row.Created)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *MetadataPolicyImportStore) Preview(ctx context.Context, input models.MetadataPolicyImportInput, now time.Time) (*models.MetadataPolicyImportPlan, error) {
	body, inputDigest, review, err := metadata.PreparePolicyImport(input, now)
	if err != nil {
		return nil, err
	}
	var canonical models.MetadataPolicyImportInput
	if err := json.Unmarshal(body, &canonical); err != nil {
		return nil, err
	}
	input = canonical
	prior, err := s.Find(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.InputSHA256 != inputDigest {
			return nil, models.ErrMetadataPolicyImportConflict
		}
		return &prior.MetadataPolicyImportPlan, nil
	}
	collection, err := (&SourceCollectionStore{}).Find(ctx, input.Policy.CollectionUUID)
	if err != nil {
		return nil, err
	}
	if collection == nil || collection.State == "retired" || collection.Revision != input.Policy.ExpectedCollectionRevision {
		return nil, models.ErrMetadataPolicyImportConflict
	}
	if input.Policy.Definition.Enabled && collection.RootUUID == nil {
		return nil, fmt.Errorf("%w: an enabled rule requires a reviewed root and folder scope", models.ErrMetadataPolicyImportInvalid)
	}
	current, err := (&MetadataPolicyStore{}).Find(ctx, collection.UUID)
	if err != nil {
		return nil, err
	}
	if (current == nil && input.Policy.ExpectedRevision != 0) || (current != nil && current.Revision != input.Policy.ExpectedRevision) {
		return nil, models.ErrMetadataPolicyImportConflict
	}
	plan := &models.MetadataPolicyImportPlan{UUID: input.UUID, InputSHA256: inputDigest, Collection: collection,
		PreviousPolicy: current, Definition: input.Policy.Definition, ReferenceRevisions: map[string]int{},
		FolderSources: input.FolderSources, ReviewKeys: review}
	for kind, rule := range input.Policy.Definition.Rules {
		for field, mapping := range rule.Mappings {
			value := mapping.TypedConstant()
			if len(value) == 0 {
				continue
			}
			revisions := map[string]int{}
			if metadataReferenceKind(field) != "" {
				var targets []*models.ArchiveEntity
				value, targets, err = resolveMetadataReferences(ctx, field, value)
				if err != nil {
					return nil, fmt.Errorf("%w: %s: %v", models.ErrMetadataPolicyImportInvalid, field, err)
				}
				for _, target := range targets {
					plan.ReferenceRevisions[target.UUID] = target.Revision
					revisions[target.UUID] = target.Revision
				}
			}
			if _, err := (&MetadataFieldStore{}).Normalize(ctx, kind, field, value, revisions); err != nil {
				return nil, fmt.Errorf("%w: %s: %v", models.ErrMetadataPolicyImportInvalid, field, err)
			}
		}
	}
	for _, ref := range input.FolderSources {
		source, err := (&SourceDocumentStore{}).Source(ctx, ref.SourceUUID)
		if err != nil {
			return nil, err
		}
		// Legacy catalogs also recorded folder documents under historical post
		// identities. Preserve that provenance; path and head establish scope.
		if source == nil || !archive.ValidRootRelativePath(source.RelativePath, false) || path.Base(source.RelativePath) != "folder.nfo" || path.Dir(source.RelativePath) != collection.PathPrefix {
			return nil, fmt.Errorf("%w: folder evidence does not describe the selected scope", models.ErrMetadataPolicyImportInvalid)
		}
		head, err := (&SourceDocumentStore{}).Head(ctx, source.CollectionUUID, source.RelativePath)
		if err != nil {
			return nil, err
		}
		if head == nil || head.UUID != ref.HeadUUID || head.State != "linked" || head.SourceUUID == nil || *head.SourceUUID != source.UUID {
			return nil, models.ErrMetadataPolicyImportConflict
		}
	}
	plan.PlanSHA256, err = metadata.PolicyImportPlanDigest(*plan)
	return plan, err
}

func (s *MetadataPolicyImportStore) Apply(ctx context.Context, input models.MetadataPolicyImportInput, expected string, now time.Time) (*models.MetadataPolicyImport, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !archive.ValidSHA256(expected) {
		return nil, models.ErrMetadataPolicyImportInvalid
	}
	body, _, _, err := metadata.PreparePolicyImport(input, now)
	if err != nil {
		return nil, err
	}
	var canonical models.MetadataPolicyImportInput
	if err := json.Unmarshal(body, &canonical); err != nil {
		return nil, err
	}
	input = canonical
	plan, err := s.Preview(ctx, input, now)
	if err != nil {
		return nil, err
	}
	if expected == "" || plan.PlanSHA256 != expected {
		return nil, models.ErrMetadataPolicyImportConflict
	}
	prior, err := s.Find(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		return &prior.MetadataPolicyImport, nil
	}
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return models.ErrMetadataPolicyImportConflict
		}
		return nil
	})
	policy, err := (&MetadataPolicyStore{}).Put(ctx, input.Policy)
	if err != nil {
		return nil, err
	}
	planBytes, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO metadata_policy_imports
(uuid,input_sha256,plan_sha256,collection_uuid,collection_revision,policy_revision,binding,plan,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		input.UUID, plan.InputSHA256, plan.PlanSHA256, plan.Collection.UUID, plan.Collection.Revision, policy.Revision, string(body), string(planBytes), now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	for _, ref := range input.FolderSources {
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO metadata_policy_import_documents(import_uuid,source_uuid,head_uuid) VALUES(?,?,?)`, input.UUID, ref.SourceUUID, ref.HeadUUID); err != nil {
			return nil, err
		}
	}
	complete = true
	return &models.MetadataPolicyImport{MetadataPolicyImportPlan: *plan, PolicyRevision: policy.Revision, CreatedAt: now.UTC()}, nil
}
