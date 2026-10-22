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
	UUID               string        `db:"uuid"`
	Revision           int           `db:"revision"`
	Label              string        `db:"label"`
	State              string        `db:"state"`
	ServerPath         *string       `db:"server_path"`
	DirectoryIdentity  *string       `db:"directory_identity"`
	UnboundPath        string        `db:"unbound_path"`
	ScanRevision       int           `db:"scan_revision"`
	ScanEnabled        bool          `db:"scan_enabled"`
	ExcludeVideo       bool          `db:"exclude_video"`
	ExcludeImage       bool          `db:"exclude_image"`
	Origin             string        `db:"origin"`
	Reason             string        `db:"reason"`
	CreatedAt          Timestamp     `db:"created_at"`
	RecordedAt         Timestamp     `db:"recorded_at"`
	PersistentIdentity *string       `db:"persistent_identity"`
	UpgradeReason      *string       `db:"upgrade_reason"`
	UpgradeRecordedAt  NullTimestamp `db:"upgrade_recorded_at"`
}

const mediaRootSelect = `SELECT b.uuid,b.created_at,r.revision,r.label,r.state,r.server_path,r.directory_identity,r.unbound_path,b.scan_revision,b.scan_enabled,b.exclude_video,b.exclude_image,r.origin,r.reason,r.created_at AS recorded_at,
u.persistent_identity,u.reason AS upgrade_reason,u.created_at AS upgrade_recorded_at
FROM media_roots b JOIN media_root_revisions r ON r.root_uuid=b.uuid
LEFT JOIN media_root_identity_upgrades u ON u.root_uuid=r.root_uuid AND u.root_revision=r.revision`

func (r mediaRootRow) resolve() *models.MediaRoot {
	ret := &models.MediaRoot{UUID: r.UUID, Revision: r.Revision, CreatedAt: r.CreatedAt.Timestamp,
		MediaRootDefinition: models.MediaRootDefinition{Label: r.Label, State: r.State, UnboundPath: r.UnboundPath,
			Scan: &models.MediaRootScan{Revision: r.ScanRevision, Enabled: r.ScanEnabled, ExcludeVideo: r.ExcludeVideo, ExcludeImage: r.ExcludeImage}}}
	if r.ServerPath != nil && r.DirectoryIdentity != nil {
		ret.Binding = &models.MediaRootBinding{Path: *r.ServerPath, DirectoryIdentity: *r.DirectoryIdentity}
		if r.PersistentIdentity != nil {
			ret.Binding.PersistentIdentity = *r.PersistentIdentity
			ret.IdentityUpgrade = &models.MediaRootIdentityUpgrade{PersistentIdentity: *r.PersistentIdentity, Reason: *r.UpgradeReason, RecordedAt: r.UpgradeRecordedAt.Timestamp}
		}
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
	if (current == nil && input.ExpectedRevision != 0) || (current != nil && current.Revision != input.ExpectedRevision) {
		return nil, models.ErrSourceDefinitionConflict
	}
	if input.Scan == nil {
		input.Scan = &models.MediaRootScan{}
		if current != nil {
			input.Scan = current.Scan
		}
	}
	if input.UnboundPath != "" && (input.Binding != nil || !validAccountText(input.UnboundPath, 4096, false) || !filepath.IsAbs(input.UnboundPath) || filepath.Clean(input.UnboundPath) != input.UnboundPath) {
		return nil, models.ErrMediaRootBindingInvalid
	}
	if input.Scan.Revision < 0 || (current == nil && input.Scan.Revision != 0) || (current != nil && input.Scan.Revision != current.Scan.Revision) {
		return nil, models.ErrSourceDefinitionConflict
	}
	// A path is configured once. A nested folder may override scan options, but
	// two logical roots cannot claim the same current local path.
	if path := input.LocalPath(); path != "" {
		var duplicate bool
		if err := dbWrapper.Get(ctx, &duplicate, `SELECT EXISTS(SELECT 1 FROM media_roots b JOIN media_root_revisions r ON r.root_uuid=b.uuid AND r.revision=b.revision
WHERE b.uuid!=? AND (r.server_path=? OR r.unbound_path=?))`, id, path, path); err != nil {
			return nil, err
		}
		if duplicate {
			return nil, fmt.Errorf("%w: folder is already registered", models.ErrSourceDefinitionConflict)
		}
	}
	// Labels and disabling an offline mount do not need that mount online.
	// A new binding or reactivation always rechecks the actual open directory.
	if input.Binding != nil {
		if current != nil && current.Binding != nil && current.Binding.PersistentIdentity != "" && input.Binding.PersistentIdentity == "" &&
			input.Binding.Path == current.Binding.Path && input.Binding.DirectoryIdentity == current.Binding.DirectoryIdentity {
			return nil, models.ErrSourceDefinitionConflict // stale client must not remove the reviewed filesystem proof
		}
		if input.Binding.PersistentIdentity != "" && (current == nil || !reflect.DeepEqual(input.Binding, current.Binding)) {
			return nil, models.ErrMediaRootBindingInvalid
		}
		if !validAccountText(input.Binding.DirectoryIdentity, 128, false) || !validAccountText(input.Binding.Path, 4096, false) || !filepath.IsAbs(input.Binding.Path) || filepath.Clean(input.Binding.Path) != input.Binding.Path {
			return nil, models.ErrMediaRootBindingInvalid
		}
		if current == nil || !reflect.DeepEqual(input.Binding, current.Binding) || (current.State != "active" && input.State == "active") {
			if err := archive.VerifyMediaRootBinding(*input.Binding); err != nil {
				return nil, fmt.Errorf("%w: %v", models.ErrMediaRootBindingInvalid, err)
			}
		}
	}
	if current != nil {
		oldDefinition, newDefinition := current.MediaRootDefinition, input.MediaRootDefinition
		oldDefinition.Scan, newDefinition.Scan = nil, nil
		if reflect.DeepEqual(oldDefinition, newDefinition) {
			if err := saveRootScan(ctx, id, current.Scan, input.Scan); err != nil {
				return nil, err
			}
			return s.Find(ctx, id)
		}
	}
	if current == nil {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO media_roots(uuid) VALUES(?)", id); err != nil {
			return nil, err
		}
	}
	// A real definition change already invalidates old admissions. Publish its
	// binding in the current format instead of carrying a compatibility namespace
	// into another revision. An identical definition returned above stays intact.
	if input.Binding != nil && input.Binding.PersistentIdentity != "" {
		binding := *input.Binding
		binding.DirectoryIdentity, binding.PersistentIdentity = binding.PersistentIdentity, ""
		input.Binding = &binding
	}
	var serverPath, identity *string
	if input.Binding != nil {
		serverPath, identity = &input.Binding.Path, &input.Binding.DirectoryIdentity
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO media_root_revisions(root_uuid,revision,label,state,server_path,directory_identity,unbound_path,origin,reason)
VALUES(?,?,?,?,?,?,?,?,?)`, id, input.ExpectedRevision+1, input.Label, input.State, serverPath, identity, input.UnboundPath, input.Origin, input.Reason); err != nil {
		return nil, err
	}
	previousScan := &models.MediaRootScan{}
	if current != nil {
		previousScan = current.Scan
	}
	if err := saveRootScan(ctx, id, previousScan, input.Scan); err != nil {
		return nil, err
	}
	return s.Find(ctx, id)
}

func saveRootScan(ctx context.Context, id string, previous, input *models.MediaRootScan) error {
	if reflect.DeepEqual(previous, input) {
		return nil
	}
	result, err := dbWrapper.Exec(ctx, `UPDATE media_roots SET scan_revision=scan_revision+1,scan_enabled=?,exclude_video=?,exclude_image=? WHERE uuid=? AND scan_revision=?`, input.Enabled, input.ExcludeVideo, input.ExcludeImage, id, input.Revision)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return models.ErrSourceDefinitionConflict
	}
	return nil
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
		root := row.resolve()
		// Binding history does not assert historical scan settings. Those use
		// an independent current revision and never alter storage authority.
		root.Scan = nil
		ret = append(ret, models.MediaRootRevision{MediaRoot: *root, Origin: row.Origin, Reason: row.Reason, RecordedAt: row.RecordedAt.Timestamp})
	}
	return ret, nil
}
