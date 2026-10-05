package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
)

type MetadataPolicyStore struct{}

type metadataPolicyRow struct {
	CollectionUUID     string    `db:"collection_uuid"`
	Revision           int       `db:"revision"`
	CollectionRevision int       `db:"collection_revision"`
	Definition         string    `db:"definition"`
	Origin             string    `db:"origin"`
	Reason             string    `db:"reason"`
	CreatedAt          Timestamp `db:"created_at"`
}

func (r metadataPolicyRow) resolve() (*models.MetadataPolicy, error) {
	ret := &models.MetadataPolicy{MetadataPolicyRef: models.MetadataPolicyRef{CollectionUUID: r.CollectionUUID, Revision: r.Revision},
		CollectionRevision: r.CollectionRevision, Origin: r.Origin, Reason: r.Reason, CreatedAt: r.CreatedAt.Timestamp}
	decoder := json.NewDecoder(strings.NewReader(r.Definition))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ret.Definition); err != nil {
		return nil, err
	}
	return ret, metadata.ValidateDefinition(ret.Definition)
}

func (s *MetadataPolicyStore) Find(ctx context.Context, collection string) (*models.MetadataPolicy, error) {
	id, err := archiveUUID(collection)
	if err != nil {
		return nil, err
	}
	var row metadataPolicyRow
	err = dbWrapper.Get(ctx, &row, `SELECT r.* FROM metadata_policies p JOIN metadata_policy_revisions r
ON r.collection_uuid=p.collection_uuid AND r.revision=p.revision WHERE p.collection_uuid=?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row.resolve()
}

func (s *MetadataPolicyStore) History(ctx context.Context, collection string, after, limit int) ([]*models.MetadataPolicy, error) {
	id, err := archiveUUID(collection)
	if err != nil {
		return nil, err
	}
	limit, err = sourcePageLimit(limit)
	if err != nil || after < 0 {
		return nil, errors.New("invalid metadata policy history page")
	}
	var rows []metadataPolicyRow
	if err := dbWrapper.Select(ctx, &rows, `SELECT * FROM metadata_policy_revisions WHERE collection_uuid=? AND revision>? ORDER BY revision LIMIT ?`, id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]*models.MetadataPolicy, 0, len(rows))
	for _, row := range rows {
		policy, err := row.resolve()
		if err != nil {
			return nil, err
		}
		ret = append(ret, policy)
	}
	return ret, nil
}

func (s *MetadataPolicyStore) Put(ctx context.Context, input models.MetadataPolicyInput) (*models.MetadataPolicy, error) {
	// Empty optional requirements have the same meaning as an omitted field.
	// Normalize without mutating the caller's map, so repeated saves remain no-ops.
	input.Definition.Rules = maps.Clone(input.Definition.Rules)
	for kind, rule := range input.Definition.Rules {
		if len(rule.OrganizedRequires) == 0 {
			rule.OrganizedRequires = nil
			input.Definition.Rules[kind] = rule
		}
	}
	if input.ExpectedRevision < 0 || input.ExpectedCollectionRevision <= 0 || (input.Origin != "review" && input.Origin != "migration") || !validAccountText(input.Reason, 4096, true) {
		return nil, fmt.Errorf("%w: invalid review", models.ErrMetadataPolicyInvalid)
	}
	if err := metadata.ValidateDefinition(input.Definition); err != nil {
		return nil, fmt.Errorf("%w: %v", models.ErrMetadataPolicyInvalid, err)
	}
	collection, err := (&SourceCollectionStore{}).Find(ctx, input.CollectionUUID)
	if err != nil {
		return nil, err
	}
	if collection == nil || collection.State == "retired" || collection.Revision != input.ExpectedCollectionRevision {
		return nil, models.ErrMetadataPolicyConflict
	}
	current, err := s.Find(ctx, collection.UUID)
	if err != nil {
		return nil, err
	}
	if (current == nil && input.ExpectedRevision != 0) || (current != nil && current.Revision != input.ExpectedRevision) {
		return nil, models.ErrMetadataPolicyConflict
	}
	for kind, rule := range input.Definition.Rules {
		for field, mapping := range rule.Mappings {
			if len(mapping.Value) == 0 || mapping.PerformerNames {
				continue
			}
			value := mapping.Value
			revisions := make(map[string]int)
			if metadataReferenceKind(field) != "" {
				var targets []*models.ArchiveEntity
				value, targets, err = resolveMetadataReferences(ctx, field, value)
				if err != nil {
					return nil, fmt.Errorf("%w: %s: %v", models.ErrMetadataPolicyInvalid, field, err)
				}
				for _, target := range targets {
					revisions[target.UUID] = target.Revision
				}
			}
			if _, err := (&MetadataFieldStore{}).Normalize(ctx, kind, field, value, revisions); err != nil {
				return nil, fmt.Errorf("%w: %s: %v", models.ErrMetadataPolicyInvalid, field, err)
			}
		}
	}
	if current != nil && current.CollectionRevision == collection.Revision && reflect.DeepEqual(current.Definition, input.Definition) {
		return current, nil
	}
	data, err := json.Marshal(input.Definition)
	if err != nil {
		return nil, err
	}
	if current == nil {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO metadata_policies(collection_uuid) VALUES(?)", collection.UUID); err != nil {
			return nil, err
		}
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO metadata_policy_revisions(collection_uuid,revision,collection_revision,definition,origin,reason)
VALUES(?,?,?,?,?,?)`, collection.UUID, input.ExpectedRevision+1, collection.Revision, string(data), input.Origin, input.Reason); err != nil {
		return nil, err
	}
	return s.Find(ctx, collection.UUID)
}

func metadataParentPaths(file string) ([]string, error) {
	if !filepath.IsAbs(file) || filepath.Clean(file) != file || len(file) > 4096 {
		return nil, errors.New("scan policy requires a canonical absolute file path")
	}
	ret := []string{}
	for directory := filepath.Dir(file); ; directory = filepath.Dir(directory) {
		ret = append(ret, directory)
		if len(ret) > 256 {
			return nil, errors.New("scan path has too many ancestors")
		}
		if filepath.Dir(directory) == directory {
			return ret, nil
		}
	}
}

func (s *MetadataPolicyStore) MatchScan(ctx context.Context, file string) ([]models.MetadataScanMatch, error) {
	parents, err := metadataParentPaths(file)
	if err != nil {
		return nil, err
	}
	args := make([]interface{}, len(parents))
	for i, parent := range parents {
		args[i] = parent
	}
	var roots []mediaRootRow
	if err := dbWrapper.Select(ctx, &roots, mediaRootSelect+` WHERE r.revision=b.revision AND r.state='active' AND r.server_path IN `+getInBinding(len(args))+` ORDER BY b.uuid LIMIT 129`, args...); err != nil {
		return nil, err
	}
	if len(roots) > 128 {
		return nil, errors.New("scan path matches too many media roots")
	}
	ret := []models.MetadataScanMatch{}
	depth := -1
	for _, row := range roots {
		root := row.resolve()
		relative, err := filepath.Rel(root.Binding.Path, file)
		if err != nil {
			return nil, err
		}
		prefixes := []interface{}{root.UUID}
		for directory := filepath.Dir(relative); ; directory = filepath.Dir(directory) {
			prefixes = append(prefixes, filepath.ToSlash(directory))
			if directory == "." {
				break
			}
		}
		var collections []sourceCollectionRow
		query := sourceCollectionSelect + ` JOIN metadata_policies p ON p.collection_uuid=b.uuid
JOIN metadata_policy_revisions pr ON pr.collection_uuid=p.collection_uuid AND pr.revision=p.revision
WHERE r.revision=b.revision AND r.state='active' AND r.root_uuid=? AND r.path_prefix IN ` + getInBinding(len(prefixes)-1) + `
AND json_extract(pr.definition,'$.apply_to_scans')=1 ORDER BY length(r.path_prefix) DESC,b.uuid LIMIT 2`
		if err := dbWrapper.Select(ctx, &collections, query, prefixes...); err != nil {
			return nil, err
		}
		for _, found := range collections {
			collection := found.resolve()
			directory := filepath.Join(root.Binding.Path, filepath.FromSlash(collection.PathPrefix))
			if len(directory) < depth {
				continue
			}
			policy, err := s.Find(ctx, collection.UUID)
			if err != nil {
				return nil, err
			}
			if len(directory) > depth {
				depth, ret = len(directory), nil
			}
			ret = append(ret, models.MetadataScanMatch{Policy: policy, Collection: collection, Root: root, RelativePath: filepath.ToSlash(relative), Directory: directory})
		}
	}
	return ret, nil
}
