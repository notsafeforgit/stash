package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type MediaRootStore struct{}

type mediaRootRow struct {
	UUID              string    `db:"uuid"`
	Revision          int       `db:"revision"`
	Label             string    `db:"label"`
	State             string    `db:"state"`
	ServerPath        *string   `db:"server_path"`
	DirectoryIdentity *string   `db:"directory_identity"`
	Origin            string    `db:"origin"`
	Reason            string    `db:"reason"`
	CreatedAt         Timestamp `db:"created_at"`
	RecordedAt        Timestamp `db:"recorded_at"`
}

const mediaRootSelect = `SELECT b.uuid,b.created_at,r.revision,r.label,r.state,r.server_path,r.directory_identity,r.origin,r.reason,r.created_at AS recorded_at
FROM media_roots b JOIN media_root_revisions r ON r.root_uuid=b.uuid`

func (r mediaRootRow) resolve() *models.MediaRoot {
	ret := &models.MediaRoot{UUID: r.UUID, Revision: r.Revision, CreatedAt: r.CreatedAt.Timestamp,
		MediaRootDefinition: models.MediaRootDefinition{Label: r.Label, State: r.State}}
	if r.ServerPath != nil && r.DirectoryIdentity != nil {
		ret.Binding = &models.MediaRootBinding{Path: *r.ServerPath, DirectoryIdentity: *r.DirectoryIdentity}
	}
	return ret
}

func sourceDefinitionID(value string, expected int) (string, error) {
	if expected < 0 || (expected > 0 && value == "") {
		return "", errors.New("source definition requires an identity and nonnegative expected revision")
	}
	if value == "" {
		return uuid.NewString(), nil
	}
	return archiveUUID(value)
}

func sourceDefinitionText(label, state, reason string) bool {
	return validAccountText(label, 1024, false) && validAccountText(reason, 4096, true) &&
		(state == "active" || state == "disabled" || state == "retired")
}

func sourceDefinitionPage(after string, limit int) (string, int, error) {
	var err error
	if after != "" {
		after, err = archiveUUID(after)
	}
	if err != nil {
		return "", 0, err
	}
	limit, err = sourcePageLimit(limit)
	return after, limit, err
}

func (s *MediaRootStore) Put(ctx context.Context, input models.MediaRootInput) (*models.MediaRoot, error) {
	id, err := sourceDefinitionID(input.UUID, input.ExpectedRevision)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", models.ErrSourceDefinitionInvalid, err)
	}
	if !sourceDefinitionText(input.Label, input.State, input.Reason) || (input.Origin != "review" && input.Origin != "migration") {
		return nil, models.ErrSourceDefinitionInvalid
	}
	current, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if (current == nil && input.ExpectedRevision != 0) || (current != nil && (current.Revision != input.ExpectedRevision || current.State == "retired")) {
		return nil, models.ErrSourceDefinitionConflict
	}
	// Labels and disabling an offline mount do not need that mount online.
	// A new binding or reactivation always rechecks the actual open directory.
	if input.Binding != nil {
		if !validAccountText(input.Binding.DirectoryIdentity, 128, false) || !validAccountText(input.Binding.Path, 4096, false) || !filepath.IsAbs(input.Binding.Path) || filepath.Clean(input.Binding.Path) != input.Binding.Path {
			return nil, models.ErrMediaRootBindingInvalid
		}
		if current == nil || !reflect.DeepEqual(input.Binding, current.Binding) || (current.State != "active" && input.State == "active") {
			if err := archive.VerifyMediaRootBinding(*input.Binding); err != nil {
				return nil, fmt.Errorf("%w: %v", models.ErrMediaRootBindingInvalid, err)
			}
		}
	}
	if current != nil && reflect.DeepEqual(current.MediaRootDefinition, input.MediaRootDefinition) {
		return current, nil
	}
	if current == nil {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO media_roots(uuid) VALUES(?)", id); err != nil {
			return nil, err
		}
	}
	var serverPath, identity *string
	if input.Binding != nil {
		serverPath, identity = &input.Binding.Path, &input.Binding.DirectoryIdentity
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO media_root_revisions(root_uuid,revision,label,state,server_path,directory_identity,origin,reason)
VALUES(?,?,?,?,?,?,?,?)`, id, input.ExpectedRevision+1, input.Label, input.State, serverPath, identity, input.Origin, input.Reason); err != nil {
		return nil, err
	}
	return s.Find(ctx, id)
}

func (s *MediaRootStore) Find(ctx context.Context, value string) (*models.MediaRoot, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	var row mediaRootRow
	if err := dbWrapper.Get(ctx, &row, mediaRootSelect+" WHERE b.uuid=? AND r.revision=b.revision", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

func (s *MediaRootStore) List(ctx context.Context, after string, limit int) ([]*models.MediaRoot, error) {
	after, limit, err := sourceDefinitionPage(after, limit)
	if err != nil {
		return nil, err
	}
	var rows []mediaRootRow
	if err := dbWrapper.Select(ctx, &rows, mediaRootSelect+" WHERE b.uuid>? AND r.revision=b.revision ORDER BY b.uuid LIMIT ?", after, limit); err != nil {
		return nil, err
	}
	ret := make([]*models.MediaRoot, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, row.resolve())
	}
	return ret, nil
}

func (s *MediaRootStore) History(ctx context.Context, value string, after, limit int) ([]models.MediaRootRevision, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	if after < 0 {
		return nil, errors.New("invalid media root revision cursor")
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	var rows []mediaRootRow
	if err := dbWrapper.Select(ctx, &rows, mediaRootSelect+" WHERE b.uuid=? AND r.revision>? ORDER BY r.revision LIMIT ?", id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.MediaRootRevision, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, models.MediaRootRevision{MediaRoot: *row.resolve(), Origin: row.Origin, Reason: row.Reason, RecordedAt: row.RecordedAt.Timestamp})
	}
	return ret, nil
}
