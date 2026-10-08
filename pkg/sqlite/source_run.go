package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	hexencoding "encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

type SourceRunStore struct{}

type sourceRunRow struct {
	Sequence           int64          `db:"id"`
	UUID               string         `db:"uuid"`
	Collection         string         `db:"collection_uuid"`
	CollectionRevision int            `db:"collection_revision"`
	Root               *string        `db:"root_uuid"`
	RootRevision       sql.NullInt64  `db:"root_revision"`
	Operation          string         `db:"operation"`
	Policy             string         `db:"policy_sha256"`
	Cooldown           int            `db:"cooldown_seconds"`
	WorkKey            string         `db:"work_key"`
	TargetKey          string         `db:"target_key"`
	Destination        string         `db:"destination"`
	RootIdentity       string         `db:"root_identity"`
	DestinationPrefix  string         `db:"destination_prefix"`
	State              string         `db:"state"`
	Revision           int64          `db:"revision"`
	Fence              int64          `db:"fence"`
	Failures           int            `db:"failures"`
	Pending            string         `db:"pending"`
	Completed          string         `db:"completed"`
	Window             sql.NullString `db:"window"`
	Producer           sql.NullString `db:"producer_uuid"`
	Owner              sql.NullString `db:"owner_uuid"`
	LeaseUntil         sql.NullInt64  `db:"lease_until_ms"`
	Available          int64          `db:"available_at_ms"`
	Progress           string         `db:"progress"`
	ErrorCode          string         `db:"error_code"`
	Created            int64          `db:"created_at_ms"`
	Updated            int64          `db:"updated_at_ms"`
}

func (row sourceRunRow) resolve(ctx context.Context) (*models.SourceRun, error) {
	var definition struct {
		TargetURL  string `db:"target_url"`
		PathPrefix string `db:"path_prefix"`
	}
	if err := dbWrapper.Get(ctx, &definition, "SELECT target_url,path_prefix FROM source_collection_revisions WHERE collection_uuid=? AND revision=?", row.Collection, row.CollectionRevision); err != nil {
		return nil, err
	}
	r := &models.SourceRun{Sequence: row.Sequence, UUID: row.UUID, CollectionUUID: row.Collection, CollectionRevision: row.CollectionRevision,
		TargetURL: definition.TargetURL, PathPrefix: definition.PathPrefix,
		RootUUID: row.Root, RootRevision: int(row.RootRevision.Int64), Operation: row.Operation, PolicySHA256: row.Policy, CooldownSeconds: row.Cooldown,
		State: row.State, Revision: row.Revision, Fence: row.Fence, Failures: row.Failures, ProducerUUID: row.Producer.String, OwnerUUID: row.Owner.String,
		AvailableAt: time.UnixMilli(row.Available).UTC(), ErrorCode: row.ErrorCode, CreatedAt: time.UnixMilli(row.Created).UTC(), UpdatedAt: time.UnixMilli(row.Updated).UTC()}
	for _, item := range []struct {
		body string
		out  any
	}{{row.Pending, &r.Pending}, {row.Completed, &r.Completed}, {row.Progress, &r.Progress}} {
		if err := json.Unmarshal([]byte(item.body), item.out); err != nil {
			return nil, err
		}
	}
	if row.Window.Valid {
		if err := json.Unmarshal([]byte(row.Window.String), &r.Window); err != nil {
			return nil, err
		}
	}
	if row.LeaseUntil.Valid {
		value := time.UnixMilli(row.LeaseUntil.Int64).UTC()
		r.LeaseUntil = &value
	}
	if r.State == "running" {
		var started int64
		if err := dbWrapper.Get(ctx, &started, "SELECT started_at_ms FROM source_run_attempts WHERE run_uuid=? AND fence=?", r.UUID, r.Fence); err != nil {
			return nil, err
		}
		until := time.UnixMilli(started).UTC().Add(sourceDownloadTurn)
		r.TurnUntil = &until
	}
	activation, err := sourceRunRecoverySeed(ctx, r.UUID)
	if err != nil {
		return nil, err
	}
	if activation != nil {
		r.Recovery = &models.SourceRunRecovery{ActivationUUID: activation.UUID, ReplayArchive: activation.ReplayArchive}
		if r.Window != nil && !scrape.SameWindow(*r.Window, activation.Window) {
			r.Recovery.ReplayArchive = true
		}
	}
	return r, nil
}

func findSourceRun(ctx context.Context, query string, args ...any) (*sourceRunRow, error) {
	var row sourceRunRow
	if err := dbWrapper.Get(ctx, &row, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func validSourceRunUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func (s *SourceRunStore) Find(ctx context.Context, id string) (*models.SourceRun, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrSourceRunInvalid
	}
	row, err := findSourceRun(ctx, "SELECT * FROM source_runs WHERE uuid=?", id)
	if err != nil || row == nil {
		return nil, err
	}
	return row.resolve(ctx)
}

func sourceRunHash(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hexencoding.EncodeToString(sum[:]), nil
}

func sourceRunDefinition(ctx context.Context, r *models.SourceRun) (*models.SourceCollection, *models.MediaRoot, error) {
	c, err := (&SourceCollectionStore{}).Find(ctx, r.CollectionUUID)
	if err != nil {
		return nil, nil, err
	}
	if c == nil || c.State != "active" || c.Revision != r.CollectionRevision || c.TargetURL == "" ||
		(c.RootUUID == nil) != (r.RootUUID == nil) || (c.RootUUID != nil && *c.RootUUID != *r.RootUUID) {
		return nil, nil, models.ErrSourceDefinitionConflict
	}
	var root *models.MediaRoot
	if c.RootUUID != nil {
		root, err = (&MediaRootStore{}).Find(ctx, *c.RootUUID)
		if err != nil {
			return nil, nil, err
		}
		if root == nil || root.State != "active" || root.Revision != r.RootRevision {
			return nil, nil, models.ErrSourceDefinitionConflict
		}
	}
	return c, root, nil
}

func (s *SourceRunStore) Submit(ctx context.Context, producer string, input models.SourceRunRequest, now time.Time, maxActive int) (*models.SourceRun, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validJobTime(now) || !validSourceRunUUID(producer) || !validSourceRunUUID(input.RequestUUID) || !validSourceRunUUID(input.CollectionUUID) ||
		input.CollectionRevision < 1 || !archive.ValidSHA256(input.PolicySHA256) || (input.Operation != "download" && input.Operation != "enrich") ||
		input.CooldownSeconds < 0 || input.CooldownSeconds > 86400 || maxActive < 1 || maxActive > 100000 {
		return nil, models.ErrSourceRunInvalid
	}
	window, err := scrape.NormalizeWindow(input.Window)
	if err != nil || (window.Basis == scrape.TraversalBasis && input.Operation != "download") {
		return nil, models.ErrSourceRunInvalid
	}
	input.Window = window
	digest, err := sourceRunHash(input)
	if err != nil {
		return nil, err
	}
	var prior struct {
		Digest string `db:"digest"`
		Run    string `db:"run_uuid"`
	}
	err = dbWrapper.Get(ctx, &prior, "SELECT digest,run_uuid FROM source_run_requests WHERE producer_uuid=? AND request_uuid=?", producer, input.RequestUUID)
	if err == nil {
		if prior.Digest != digest {
			return nil, models.ErrSourceRunConflict
		}
		return s.Find(ctx, prior.Run)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if window.Until.After(now.Add(time.Minute)) {
		return nil, models.ErrSourceRunInvalid
	}
	run, _, err := s.enqueue(ctx, input, now, maxActive)
	if err != nil {
		return nil, err
	}
	id := run.UUID
	if _, err = dbWrapper.Exec(ctx, "INSERT INTO source_run_requests(producer_uuid,request_uuid,digest,run_uuid,created_at_ms) VALUES(?,?,?,?,?)", producer, input.RequestUUID, digest, id, now.UnixMilli()); err != nil {
		return nil, err
	}
	return s.Find(ctx, id)
}

// enqueue is shared by producer admission and application-reviewed recovery.
// The caller validates identity, window, policy, capacity and its transaction.
func (s *SourceRunStore) enqueue(ctx context.Context, input models.SourceRunRequest, now time.Time, maxActive int) (*models.SourceRun, bool, error) {
	c, err := (&SourceCollectionStore{}).Find(ctx, input.CollectionUUID)
	if err != nil {
		return nil, false, err
	}
	if c == nil || c.Revision != input.CollectionRevision || c.State != "active" || c.TargetURL == "" ||
		(input.Operation == "enrich" && c.Namespace == "") ||
		(input.Operation == "download" && c.RootUUID == nil) {
		return nil, false, models.ErrSourceDefinitionConflict
	}
	var rootRevision any
	if c.RootUUID != nil {
		root, err := (&MediaRootStore{}).Find(ctx, *c.RootUUID)
		if err != nil {
			return nil, false, err
		}
		if root == nil || root.State != "active" {
			return nil, false, models.ErrSourceDefinitionConflict
		}
		rootRevision = root.Revision
	}
	workDefinition := []any{c.UUID, c.Revision, rootRevision, input.Operation, input.PolicySHA256, input.CooldownSeconds}
	if input.Window.Basis != "" {
		workDefinition = append(workDefinition, input.Window.Basis)
	}
	work, err := sourceRunHash(workDefinition)
	if err != nil {
		return nil, false, err
	}
	u, err := url.Parse(c.TargetURL)
	if err != nil {
		return nil, false, models.ErrSourceDefinitionConflict
	}
	u.Host, u.Scheme, u.Fragment = strings.ToLower(u.Host), strings.ToLower(u.Scheme), ""
	target, err := sourceRunHash([]string{c.Namespace, u.String()})
	if err != nil {
		return nil, false, err
	}
	row, err := findSourceRun(ctx, "SELECT * FROM source_runs WHERE work_key=? AND state IN ('queued','running','deferred')", work)
	if err != nil {
		return nil, false, err
	}
	created := row == nil
	id := uuid.NewString()
	if row != nil {
		r, err := row.resolve(ctx)
		if err != nil {
			return nil, false, err
		}
		covered := r.Completed
		active := 0
		if r.Window != nil {
			covered = scrape.Union(covered, []models.SourceWindow{*r.Window})
			active = 1
		}
		r.Pending = scrape.Subtract(scrape.Union(r.Pending, []models.SourceWindow{input.Window}), covered)
		if len(r.Pending)+len(r.Completed)+active > scrape.MaxWindows {
			return nil, false, models.ErrSourceRunCapacity
		}
		pending, err := json.Marshal(r.Pending)
		if err != nil {
			return nil, false, err
		}
		// A timer cannot reset backoff, a durable deferral, or running ownership.
		if string(pending) != row.Pending {
			if _, err = dbWrapper.Exec(ctx, "UPDATE source_runs SET pending=?,revision=revision+1,updated_at_ms=? WHERE uuid=?", string(pending), now.UnixMilli(), row.UUID); err != nil {
				return nil, false, err
			}
		}
		id = row.UUID
	} else {
		var active int
		if err := dbWrapper.Get(ctx, &active, "SELECT count(*) FROM (SELECT 1 FROM source_runs WHERE state IN ('queued','running','deferred') LIMIT ?)", maxActive); err != nil {
			return nil, false, err
		}
		if active >= maxActive {
			return nil, false, models.ErrSourceRunCapacity
		}
		pending, err := json.Marshal([]models.SourceWindow{input.Window})
		if err != nil {
			return nil, false, err
		}
		_, err = dbWrapper.Exec(ctx, `INSERT INTO source_runs(uuid,collection_uuid,collection_revision,root_uuid,root_revision,operation,policy_sha256,cooldown_seconds,work_key,target_key,pending,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, c.UUID, c.Revision, c.RootUUID, rootRevision, input.Operation, input.PolicySHA256, input.CooldownSeconds, work, target, string(pending), now.UnixMilli(), now.UnixMilli(), now.UnixMilli())
		if err != nil {
			return nil, false, err
		}
	}
	run, err := s.Find(ctx, id)
	return run, created, err
}

func (s *SourceRunStore) List(ctx context.Context, collection string, roots []*string, after int64, limit int) ([]models.SourceRun, error) {
	if !validSourceRunUUID(collection) || len(roots) < 1 || len(roots) > 128 || after < 0 {
		return nil, models.ErrSourceRunInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrSourceRunInvalid
	}
	args := []any{collection, after}
	for _, root := range roots {
		if root != nil && !validSourceRunUUID(*root) {
			return nil, models.ErrSourceRunInvalid
		}
		args = append(args, root)
	}
	args = append(args, limit)
	query := "SELECT * FROM source_runs WHERE collection_uuid=? AND id>? AND (" +
		strings.TrimSuffix(strings.Repeat("root_uuid IS ? OR ", len(roots)), " OR ") + ") ORDER BY id LIMIT ?"
	var rows []sourceRunRow
	if err := dbWrapper.Select(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	ret := make([]models.SourceRun, 0, len(rows))
	for _, row := range rows {
		r, err := row.resolve(ctx)
		if err != nil {
			return nil, err
		}
		ret = append(ret, *r)
	}
	return ret, nil
}

func (s *SourceRunStore) Ready(ctx context.Context, collections []string, allCollections bool, root, policy string, after int64, limit int, now time.Time) ([]models.SourceRunCandidate, error) {
	if (len(collections) < 1 && !allCollections) || len(collections) > 128 || !validSourceRunUUID(root) || !archive.ValidSHA256(policy) || after < 0 || !validJobTime(now) {
		return nil, models.ErrSourceRunInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrSourceRunInvalid
	}
	args := []any{root, policy, after, now.UnixMilli(), now.UnixMilli()}
	for _, collection := range collections {
		if !validSourceRunUUID(collection) {
			return nil, models.ErrSourceRunInvalid
		}
		if !allCollections {
			args = append(args, collection)
		}
	}
	args = append(args, limit)
	// Use the bounded active set, never traverse completed run history. An
	// expired attempt is only a hint: Claim performs recovery and backoff in
	// its write transaction before considering ownership. Deferred work stays
	// behind the explicit review boundary.
	query := `SELECT id,uuid FROM source_runs INDEXED BY source_runs_active
WHERE state IN ('queued','running','deferred') AND root_uuid=? AND policy_sha256=? AND id>? AND operation='download'
AND ((state='queued' AND available_at_ms<=?) OR (state='running' AND lease_until_ms<=?))`
	if !allCollections {
		query += " AND collection_uuid IN (" + strings.TrimSuffix(strings.Repeat("?,", len(collections)), ",") + ")"
	}
	query += " ORDER BY id LIMIT ?"
	ret := make([]models.SourceRunCandidate, 0)
	if err := dbWrapper.Select(ctx, &ret, query, args...); err != nil {
		return nil, err
	}
	return ret, nil
}

func (s *SourceRunStore) Attempts(ctx context.Context, id string, after int64, limit int) ([]models.SourceRunAttempt, error) {
	if !validSourceRunUUID(id) || after < 0 {
		return nil, models.ErrSourceRunInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrSourceRunInvalid
	}
	var rows []struct {
		Run        string        `db:"run_uuid"`
		Fence      int64         `db:"fence"`
		Producer   string        `db:"producer_uuid"`
		Owner      string        `db:"owner_uuid"`
		Window     string        `db:"window"`
		Progress   string        `db:"progress"`
		Started    int64         `db:"started_at_ms"`
		Ended      sql.NullInt64 `db:"ended_at_ms"`
		Outcome    string        `db:"outcome"`
		Error      string        `db:"error_code"`
		ErrorScope string        `db:"error_scope"`
	}
	if err := dbWrapper.Select(ctx, &rows, `SELECT a.*,coalesce(f.scope,'') AS error_scope FROM source_run_attempts a
LEFT JOIN source_run_attempt_failures f ON f.run_uuid=a.run_uuid AND f.fence=a.fence
WHERE a.run_uuid=? AND a.fence>? ORDER BY a.fence LIMIT ?`, id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.SourceRunAttempt, 0, len(rows))
	for _, row := range rows {
		a := models.SourceRunAttempt{SourceRunLease: models.SourceRunLease{RunUUID: row.Run, Fence: row.Fence, ProducerUUID: row.Producer, OwnerUUID: row.Owner}, StartedAt: time.UnixMilli(row.Started).UTC(), Outcome: row.Outcome, ErrorCode: row.Error, ErrorScope: row.ErrorScope}
		if row.Ended.Valid {
			value := time.UnixMilli(row.Ended.Int64).UTC()
			a.EndedAt = &value
		}
		if err := json.Unmarshal([]byte(row.Window), &a.Window); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(row.Progress), &a.Progress); err != nil {
			return nil, err
		}
		ret = append(ret, a)
	}
	return ret, nil
}
