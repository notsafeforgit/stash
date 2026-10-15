package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func validTranslationNamespace(value string) bool {
	return strings.HasPrefix(value, "native:") && len(value) > 7 && validAccountText(value, 128, false) && strings.TrimSpace(value) == value
}

func (s *TranslationPolicyStore) Default(ctx context.Context, namespace string) (*models.TranslationPolicyDefault, error) {
	if !validTranslationNamespace(namespace) {
		return nil, models.ErrTranslationPolicyInvalid
	}
	var row struct {
		Namespace  string `db:"namespace"`
		Revision   int    `db:"revision"`
		Definition string `db:"definition"`
	}
	err := dbWrapper.Get(ctx, &row, "SELECT * FROM translation_policy_defaults WHERE namespace=?", namespace)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	definition, err := archive.DecodeTranslationPolicy([]byte(row.Definition))
	if err != nil {
		return nil, err
	}
	return &models.TranslationPolicyDefault{Namespace: row.Namespace, Revision: row.Revision, Definition: definition}, nil
}

// Revision is the reviewed current revision (zero to create), not the new one.
func (s *TranslationPolicyStore) PutDefault(ctx context.Context, input models.TranslationPolicyDefault) (*models.TranslationPolicyDefault, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if input.Revision < 0 || archive.ValidateTranslationPolicy(input.Definition) != nil {
		return nil, models.ErrTranslationPolicyInvalid
	}
	prior, err := s.Default(ctx, input.Namespace)
	if err != nil {
		return nil, err
	}
	if (prior == nil && input.Revision != 0) || (prior != nil && prior.Revision != input.Revision) {
		return nil, models.ErrTranslationPolicyConflict
	}
	if prior != nil && prior.Definition == input.Definition {
		return prior, nil
	}
	definition, err := json.Marshal(input.Definition)
	if err != nil {
		return nil, err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO translation_policy_defaults(namespace,revision,definition) VALUES(?,?,?)
ON CONFLICT(namespace) DO UPDATE SET revision=excluded.revision,definition=excluded.definition`, input.Namespace, input.Revision+1, string(definition))
	if err != nil {
		return nil, err
	}
	return s.Default(ctx, input.Namespace)
}

func (s *TranslationPolicyStore) initializeDefault(ctx context.Context, collection *models.SourceCollection) (*models.TranslationPolicy, error) {
	if collection == nil || collection.State != "active" || !validTranslationNamespace(collection.Namespace) {
		return nil, nil
	}
	policy, err := s.Default(ctx, collection.Namespace)
	if err != nil || policy == nil {
		return nil, err
	}
	return s.Put(ctx, models.TranslationPolicyInput{CollectionUUID: collection.UUID, ExpectedCollectionRevision: collection.Revision,
		Definition: policy.Definition, Origin: "review", Reason: "Initialized from the selected namespace translation default"})
}
