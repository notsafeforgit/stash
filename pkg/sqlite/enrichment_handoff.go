package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"sort"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

type enrichmentRetainedRecord struct {
	Ordinal     int    `db:"ordinal"`
	CaptureUUID string `db:"capture_uuid"`
	Digest      string `db:"digest"`
}

func enrichmentSeedServices(seed *archive.EnrichmentTranscript) ([]string, error) {
	set := map[string]bool{}
	for _, ref := range seed.Pending {
		scope, err := scrape.SourceScopeV1(ref.URL)
		if err != nil {
			return nil, err
		}
		// The child metadata collector contacts only these supported services.
		if scope == "service:redgifs" || scope == "service:imgur" {
			set[scope] = true
		}
	}
	ret := make([]string, 0, len(set))
	for scope := range set {
		ret = append(ret, scope)
	}
	sort.Strings(ret)
	return ret, nil
}

func (s *EnrichmentJobStore) HandoffJob(ctx context.Context, id string) (*models.ArchiveJob, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrEnrichmentInvalid
	}
	var job string
	err := dbWrapper.Get(ctx, &job, "SELECT job_uuid FROM enrichment_handoff_jobs WHERE handoff_uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return (&ArchiveJobStore{}).Find(ctx, job)
}

func (s *EnrichmentJobStore) AdmitHandoff(ctx context.Context, id, expected string, now time.Time) (*models.ArchiveJob, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(id) || !archive.ValidSHA256(expected) || !validJobTime(now) {
		return nil, models.ErrEnrichmentInvalid
	}
	prior, err := s.HandoffJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		work, err := archive.DecodeEnrichmentJob(prior)
		if err != nil {
			return nil, err
		}
		if work.Handoff == nil || work.Handoff.UUID != id || work.Handoff.PlanSHA256 != expected {
			return nil, models.ErrEnrichmentConflict
		}
		return prior, nil
	}
	store := &AutomationCheckpointImportStore{}
	reviewed, err := store.Handoff(ctx, id)
	if err != nil {
		return nil, err
	}
	if reviewed == nil || reviewed.PlanSHA256 != expected || now.Before(reviewed.CreatedAt) {
		return nil, models.ErrEnrichmentConflict
	}
	plan, err := store.PreviewHandoff(ctx, reviewed.Input)
	if err != nil {
		return nil, err
	}
	if plan.PlanSHA256 != expected {
		return nil, models.ErrEnrichmentConflict
	}
	seed, err := store.HandoffSeed(ctx, id)
	if err != nil {
		return nil, err
	}
	parsed, err := archive.ParseEnrichmentTranscript(seed.Body)
	if err != nil {
		return nil, err
	}
	services, err := enrichmentSeedServices(parsed)
	if err != nil {
		return nil, err
	}
	complete := enrichmentAtomic(ctx)
	workStore := &EnrichmentWorkStore{}
	schedule := plan.Target.EnrichmentSchedule
	schedule.State, schedule.Reason = "pending", ""
	var target *models.EnrichmentTarget
	if plan.ReleasedTargetUUID == plan.Target.UUID {
		target, err = workStore.Schedule(ctx, plan.Target.UUID, plan.Target.Revision, schedule, now)
	} else {
		excluded := schedule
		excluded.State, excluded.Reason = "excluded", "checkpoint_handoff"
		if _, err = workStore.Schedule(ctx, plan.Target.UUID, plan.Target.Revision, excluded, now); err != nil {
			return nil, err
		}
		input := plan.Target.EnrichmentTargetInput
		input.CollectionRevision, input.Origin = plan.Collection.Revision, "review"
		target, err = workStore.RetainTarget(ctx, input, schedule, now)
	}
	if err != nil {
		return nil, err
	}
	if target.UUID != plan.ReleasedTargetUUID || target.Revision != plan.ReleasedRevision {
		return nil, models.ErrEnrichmentConflict
	}
	input, err := archive.PrepareEnrichmentJob(models.EnrichmentJobArguments{Version: 2, TargetUUID: target.UUID, TargetRevision: target.Revision,
		PostUUID: target.PostUUID, CollectionUUID: plan.Collection.UUID, CollectionRevision: plan.Collection.Revision, RootUUID: plan.Collection.RootUUID,
		PolicySHA256: plan.Input.PolicySHA256, ExtractorVersion: plan.Input.ExtractorVersion, CapturePolicy: plan.CapturePolicy,
		Handoff: &models.EnrichmentJobHandoff{UUID: id, PlanSHA256: expected, SeedSHA256: plan.SeedSHA256}})
	if err != nil {
		return nil, err
	}
	input.Priority, input.AvailableAt = target.Priority, target.NotBefore
	job, err := (&ArchiveJobStore{}).Submit(ctx, input, now, 10000)
	if err != nil {
		return nil, err
	}
	if err := s.Bind(ctx, job.UUID, now); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO enrichment_handoff_jobs VALUES(?,?,?,?)", id, job.UUID, plan.Target.Revision+1, now.UTC()); err != nil {
		return nil, err
	}
	for ordinal, record := range parsed.Records {
		digest, err := parsed.RecordDigest(ordinal)
		if err != nil || record.RetainedCapture == nil {
			return nil, models.ErrSourcePayloadCorrupt
		}
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO enrichment_job_retained_records VALUES(?,?,?,?)", job.UUID, ordinal, *record.RetainedCapture, digest); err != nil {
			return nil, err
		}
	}
	for _, scope := range services {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO enrichment_job_seed_services VALUES(?,?)", job.UUID, scope); err != nil {
			return nil, err
		}
	}
	*complete = true
	return job, nil
}

// Read the immutable execution contract against original review/evidence, not
// current scheduling state. A completed or cancelled job remains recoverable.
func readEnrichmentSeed(get enrichmentGet, selectRows enrichmentSelect, job *models.ArchiveJob, work *models.EnrichmentJobArguments) (*models.CheckpointHandoffSeed, error) {
	if work.Handoff == nil {
		return nil, nil
	}
	var binding struct {
		HandoffUUID      string    `db:"handoff_uuid"`
		JobUUID          string    `db:"job_uuid"`
		ConsumedRevision int       `db:"consumed_revision"`
		CreatedAt        time.Time `db:"created_at"`
	}
	if err := get(&binding, "SELECT * FROM enrichment_handoff_jobs WHERE job_uuid=?", job.UUID); err != nil {
		return nil, err
	}
	receipt, body, err := readCheckpointHandoff(get, selectRows, work.Handoff.UUID)
	if err != nil {
		return nil, err
	}
	if receipt == nil || binding.HandoffUUID != receipt.Input.UUID || binding.ConsumedRevision != receipt.Target.Revision+1 ||
		binding.CreatedAt.Before(receipt.CreatedAt) || job.CreatedAt.UnixMilli() != binding.CreatedAt.UnixMilli() ||
		receipt.PlanSHA256 != work.Handoff.PlanSHA256 || receipt.SeedSHA256 != work.Handoff.SeedSHA256 ||
		work.Version != 2 || work.CapturePolicy != receipt.CapturePolicy || work.TargetUUID != receipt.ReleasedTargetUUID || work.TargetRevision != receipt.ReleasedRevision ||
		work.PostUUID != receipt.Target.PostUUID || work.CollectionUUID != receipt.Collection.UUID || work.CollectionRevision != receipt.Collection.Revision ||
		!reflect.DeepEqual(work.RootUUID, receipt.Collection.RootUUID) || work.PolicySHA256 != receipt.Input.PolicySHA256 || work.ExtractorVersion != receipt.Input.ExtractorVersion {
		return nil, models.ErrSourcePayloadCorrupt
	}
	var source, released models.EnrichmentTargetHistory
	if err := get(&source, "SELECT * FROM enrichment_target_history WHERE target_uuid=? AND revision=?", receipt.Target.UUID, binding.ConsumedRevision); err != nil {
		return nil, err
	}
	if err := get(&released, "SELECT * FROM enrichment_target_history WHERE target_uuid=? AND revision=?", work.TargetUUID, work.TargetRevision); err != nil {
		return nil, err
	}
	state, reason := "pending", ""
	if work.TargetUUID != receipt.Target.UUID {
		state, reason = "excluded", "checkpoint_handoff"
	}
	if source.State != state || source.Reason != reason || !source.RecordedAt.Equal(binding.CreatedAt) || source.CompletionUUID != nil ||
		released.State != "pending" || released.Reason != "" || !released.RecordedAt.Equal(binding.CreatedAt) || released.CompletionUUID != nil ||
		source.Priority != receipt.Target.Priority || !source.NotBefore.Equal(receipt.Target.NotBefore) || released.Priority != source.Priority || !released.NotBefore.Equal(source.NotBefore) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	parsed, err := archive.ParseEnrichmentTranscript(body)
	if err != nil {
		return nil, err
	}
	var records []enrichmentRetainedRecord
	if err := selectRows(&records, "SELECT ordinal,capture_uuid,digest FROM enrichment_job_retained_records WHERE job_uuid=? ORDER BY ordinal LIMIT 1025", job.UUID); err != nil {
		return nil, err
	}
	if len(records) != len(parsed.Records) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	for i, r := range records {
		digest, err := parsed.RecordDigest(i)
		if err != nil || r.Ordinal != i || parsed.Records[i].RetainedCapture == nil || r.CaptureUUID != *parsed.Records[i].RetainedCapture || r.Digest != digest {
			return nil, models.ErrSourcePayloadCorrupt
		}
	}
	expected, err := enrichmentSeedServices(parsed)
	if err != nil {
		return nil, err
	}
	services := []string{}
	if err := selectRows(&services, "SELECT scope FROM enrichment_job_seed_services WHERE job_uuid=? ORDER BY scope LIMIT 257", job.UUID); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(services, expected) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return &models.CheckpointHandoffSeed{HandoffUUID: work.Handoff.UUID, PlanSHA256: receipt.PlanSHA256, SHA256: receipt.SeedSHA256, Body: body}, nil
}

func (s *EnrichmentJobStore) Seed(ctx context.Context, id string) (*models.CheckpointHandoffSeed, error) {
	job, err := (&ArchiveJobStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	work, err := archive.DecodeEnrichmentJob(job)
	if err != nil {
		return nil, err
	}
	return readEnrichmentSeed(func(out any, q string, args ...any) error { return dbWrapper.Get(ctx, out, q, args...) },
		func(out any, q string, args ...any) error { return dbWrapper.Select(ctx, out, q, args...) }, job, work)
}

func verifyEnrichmentTranscript(get enrichmentGet, selectRows enrichmentSelect, job *models.ArchiveJob, work *models.EnrichmentJobArguments, parsed *archive.EnrichmentTranscript) error {
	if parsed == nil || parsed.ExtractorVersion != work.ExtractorVersion {
		return models.ErrEnrichmentConflict
	}
	if work.Handoff == nil {
		if parsed.Schema != archive.EnrichmentTranscriptSchema {
			return models.ErrEnrichmentInvalid
		}
		return nil
	}
	seed, err := readEnrichmentSeed(get, selectRows, job, work)
	if err != nil {
		return err
	}
	if seed == nil {
		return models.ErrSourcePayloadCorrupt
	}
	before, err := archive.ParseEnrichmentTranscript(seed.Body)
	if err != nil {
		return err
	}
	if parsed.Schema != archive.EnrichmentRetainedSchema || !parsed.Extends(before) {
		return models.ErrEnrichmentConflict
	}
	return nil
}

func enrichmentRecordCapture(get enrichmentGet, selectRows enrichmentSelect, job string, work *models.EnrichmentJobArguments, transcript *archive.EnrichmentTranscript,
	record models.EnrichmentCheckpointRecord, preceding []string) (*models.SourceCaptureInput, *models.SourcePostIdentifier, error) {
	if record.Ordinal < 0 || record.Ordinal >= len(transcript.Records) {
		return nil, nil, models.ErrSourcePayloadCorrupt
	}
	var parent string
	if ordinal := transcript.Records[record.Ordinal].Parent; ordinal != nil {
		if *ordinal < 0 || *ordinal >= record.Ordinal || *ordinal >= len(preceding) || preceding[*ordinal] == "" {
			return nil, nil, models.ErrSourcePayloadCorrupt
		}
		parent = preceding[*ordinal]
	}
	var retained *models.SourceCapture
	if work.Handoff != nil {
		var r enrichmentRetainedRecord
		err := get(&r, "SELECT ordinal,capture_uuid,digest FROM enrichment_job_retained_records WHERE job_uuid=? AND ordinal=?", job, record.Ordinal)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, err
		}
		if err == nil {
			if r.Digest != record.Digest {
				return nil, nil, models.ErrSourcePayloadCorrupt
			}
			record.RetainedCapture = &r.CaptureUUID
			retained, err = findSourceCapture(get, selectRows, r.CaptureUUID)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	return archive.PrepareEnrichmentRecord(job, work, transcript, record, parent, retained)
}
