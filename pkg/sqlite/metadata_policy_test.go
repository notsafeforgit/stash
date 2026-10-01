package sqlite_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

type metadataPolicyFixture struct {
	db         *sqlite.Database
	repo       models.Repository
	root       *models.MediaRoot
	collection *models.SourceCollection
	entity     *models.ArchiveEntity
}

func newMetadataPolicyFixture(t *testing.T) metadataPolicyFixture {
	t.Helper()
	db, repo := archiveTestDatabase(t)
	binding, err := archive.ProbeMediaRoot(t.TempDir())
	require.NoError(t, err)
	root := putMediaRoot(t, repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Manual files", State: "active", Binding: binding}})
	collection := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Purchased", Kind: "directory", State: "active", RootUUID: &root.UUID, PathPrefix: "."}})
	attachmentSQL(t, db, "INSERT INTO scenes(id,created_at,updated_at) VALUES(33,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)")
	return metadataPolicyFixture{db, repo, root, collection, archiveFind(t, repo, models.ArchiveScene, 33)}
}

func (f metadataPolicyFixture) put(t *testing.T, revision int, rule models.MetadataPolicyRule) *models.MetadataPolicy {
	t.Helper()
	var policy *models.MetadataPolicy
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		policy, err = f.repo.MetadataPolicy.Put(ctx, models.MetadataPolicyInput{CollectionUUID: f.collection.UUID, ExpectedCollectionRevision: f.collection.Revision, ExpectedRevision: revision, Origin: "review",
			Definition: models.MetadataPolicyDefinition{Enabled: true, ApplyToScans: true, Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{models.ArchiveScene: rule}}})
		return err
	}))
	return policy
}

func (f metadataPolicyFixture) input(policy *models.MetadataPolicy) metadata.Input {
	return metadata.Input{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, PolicyRevision: policy.Revision, EntityUUID: f.entity.UUID, Created: true, RelativePath: "A purchased file.mp4"}
}

func policyPreview(t *testing.T, repo models.Repository, input metadata.Input) *metadata.Preview {
	t.Helper()
	var result *metadata.Preview
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = (metadata.Service{Repo: repo}).Preview(ctx, input)
		return err
	}))
	return result
}

func applyPolicy(repo models.Repository, input metadata.Input, digest string) (*metadata.Preview, error) {
	var result *metadata.Preview
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		result, err = (metadata.Service{Repo: repo}).Apply(ctx, input, digest)
		return err
	})
	return result, err
}

func policyChange(t *testing.T, preview *metadata.Preview, field string) metadata.Change {
	t.Helper()
	for i := len(preview.Changes) - 1; i >= 0; i-- {
		if preview.Changes[i].Field == field {
			return preview.Changes[i]
		}
	}
	t.Fatalf("no policy change for %s: %+v", field, preview)
	return metadata.Change{}
}

func TestMetadataPolicyManualPreviewApplyProtectionAndRestart(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	performer := archiveFind(t, f.repo, models.ArchivePerformer, 71)
	rule := models.MetadataPolicyRule{OnCreate: true, OnExisting: true, FilenameTitleFallback: true, MarkOrganized: true, Mappings: map[string]models.MetadataMapping{
		"title": {JQ: `.source.metadata.title // empty`}, "performers": {Value: json.RawMessage(`["` + performer.UUID + `"]`)},
	}}
	policy := f.put(t, 0, rule)
	input := f.input(policy)
	before := metadataHistory(t, f.repo, f.entity.UUID, "title")
	preview := policyPreview(t, f.repo, input)
	require.Equal(t, "ready", preview.State)
	require.JSONEq(t, `"A purchased file"`, string(policyChange(t, preview, "title").Value))
	require.Equal(t, before, metadataHistory(t, f.repo, f.entity.UUID, "title"), "preview must not write decisions")
	require.NotContains(t, preview.Data, "settings")
	require.NotContains(t, preview.Data, "input")
	require.NotContains(t, preview.Data, "fields")
	require.Nil(t, preview.Data["source"])
	applied, err := applyPolicy(f.repo, input, preview.Digest)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"performers", "title", "organized"}, applied.AppliedFields())
	state := metadataState(t, f.repo, f.entity.UUID, "performers")
	require.False(t, state.Protected)
	require.Equal(t, policy.MetadataPolicyRef, *state.Decision.Policy)
	require.Equal(t, "filename", metadataState(t, f.repo, f.entity.UUID, "title").Origin)
	_, err = applyPolicy(f.repo, input, preview.Digest)
	require.ErrorIs(t, err, models.ErrMetadataPolicyConflict, "old preview cannot apply twice")
	replayed, err := applyPolicy(f.repo, input, "")
	require.NoError(t, err)
	require.Empty(t, replayed.AppliedFields())
	alternativeFile := input
	alternativeFile.RelativePath = "Another copy.mp4"
	replayed, err = applyPolicy(f.repo, alternativeFile, "")
	require.NoError(t, err)
	require.Empty(t, replayed.AppliedFields(), "another path must not oscillate a shared item's filename title")
	_, err = applyMetadata(f.repo, metadataInput(metadataState(t, f.repo, f.entity.UUID, "title"), "clear", "review", ""), false)
	require.NoError(t, err)
	dbPath := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(dbPath))
	replayed, err = applyPolicy(f.repo, input, "")
	require.NoError(t, err)
	require.Equal(t, "protected", policyChange(t, replayed, "title").Status)
	require.Equal(t, `""`, string(metadataState(t, f.repo, f.entity.UUID, "title").Value))
	require.Equal(t, policy.MetadataPolicyRef, *metadataState(t, f.repo, f.entity.UUID, "performers").Decision.Policy)
}

func TestMetadataPolicyAliasCollisionsAndRedirectedDefaults(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	attachmentSQL(t, f.db, `INSERT INTO performer_names(performer_id,name,position) VALUES(71,'Unique alias',1),(72,'Unique alias',1);`)
	rule := models.MetadataPolicyRule{OnCreate: true, OnExisting: true, MarkOrganized: true, Mappings: map[string]models.MetadataMapping{"performers": {Value: json.RawMessage(`["Unique alias"]`), PerformerNames: true}}}
	policy := f.put(t, 0, rule)
	preview := policyPreview(t, f.repo, f.input(policy))
	change := policyChange(t, preview, "performers")
	require.Equal(t, "review", change.Status)
	require.Len(t, change.Names[0].Candidates, 2)
	require.Equal(t, "ambiguous", change.Names[0].Status)
	attachmentSQL(t, f.db, `DELETE FROM performer_names WHERE performer_id=72 AND position=1`)
	preview = policyPreview(t, f.repo, f.input(policy))
	require.Equal(t, "ready", policyChange(t, preview, "performers").Status)
	attachmentSQL(t, f.db, `UPDATE performer_names SET name='Unique alias' WHERE performer_id=72 AND position=0`)
	_, err := applyPolicy(f.repo, f.input(policy), preview.Digest)
	require.ErrorIs(t, err, models.ErrMetadataPolicyConflict, "a new canonical/alias collision invalidates the preview")
	p := archiveFind(t, f.repo, models.ArchivePerformer, 71)
	rule.Mappings["performers"] = models.MetadataMapping{Value: json.RawMessage(`["` + p.UUID + `"]`)}
	policy = f.put(t, policy.Revision, rule)
	newID := uuid.NewString()
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveEntity.AdoptUUID(ctx, p.UUID, newID, p.Revision)
		return err
	}))
	applied, err := applyPolicy(f.repo, f.input(policy), "")
	require.NoError(t, err)
	require.JSONEq(t, `["`+newID+`"]`, string(policyChange(t, applied, "performers").Value))
	stored, err := json.Marshal(policy.Definition)
	require.NoError(t, err)
	require.Contains(t, string(stored), p.UUID, "retain the reviewed historical reference; resolve its redirect at application")
}

func TestMetadataPolicyScopeVersionsAndTypedTargets(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	rule := models.MetadataPolicyRule{OnCreate: true, FilenameTitleFallback: true, Mappings: map[string]models.MetadataMapping{}}
	policy := f.put(t, 0, rule)
	require.Equal(t, policy, f.put(t, policy.Revision, rule))
	input := f.input(policy)
	rule.Mappings["rating100"] = models.MetadataMapping{Value: json.RawMessage(`50`)}
	next := f.put(t, policy.Revision, rule)
	require.Equal(t, "policy_changed", policyPreview(t, f.repo, input).State)
	input.PolicyRevision = next.Revision
	definition := f.collection.SourceCollectionDefinition
	definition.PathPrefix = "new-folder"
	f.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, SourceCollectionDefinition: definition, Origin: "review"})
	require.Equal(t, "collection_changed", policyPreview(t, f.repo, input).State)
	for _, field := range []string{"id", "uuid", "files", "fingerprints", "source", "capture_uuid"} {
		bad := models.MetadataPolicyDefinition{Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{models.ArchiveScene: {Mappings: map[string]models.MetadataMapping{field: {Value: json.RawMessage(`"no"`)}}}}}
		require.Error(t, metadata.ValidateDefinition(bad), field)
	}
	for _, mapping := range []models.MetadataMapping{{Value: json.RawMessage(`"wrong type"`)}, {Value: json.RawMessage(`101`)}} {
		rule.Mappings["rating100"] = mapping
		err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.MetadataPolicy.Put(ctx, models.MetadataPolicyInput{CollectionUUID: f.collection.UUID, ExpectedCollectionRevision: f.collection.Revision, ExpectedRevision: next.Revision, Origin: "review", Definition: models.MetadataPolicyDefinition{Enabled: true, Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{models.ArchiveScene: rule}}})
			return err
		})
		require.Error(t, err)
	}
}

func TestMetadataPolicyScanChoosesSpecificDirectoryAndReportsTies(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	rule := models.MetadataPolicyRule{OnCreate: true, FilenameTitleFallback: true}
	f.put(t, 0, rule)
	directory := filepath.Join(f.root.Binding.Path, "nested")
	require.NoError(t, os.Mkdir(directory, 0700))
	file := filepath.Join(directory, "file.mp4")
	var match []models.MetadataScanMatch
	lookup := func() {
		require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			match, err = f.repo.MetadataPolicy.MatchScan(ctx, file)
			return err
		}))
	}
	lookup()
	require.Len(t, match, 1)
	require.Equal(t, f.collection.UUID, match[0].Collection.UUID)
	inner := f
	inner.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Nested", Kind: "directory", State: "active", RootUUID: &f.root.UUID, PathPrefix: "nested"}})
	inner.put(t, 0, rule)
	lookup()
	require.Len(t, match, 1)
	require.Equal(t, inner.collection.UUID, match[0].Collection.UUID)
	inner.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: inner.collection.SourceCollectionDefinition})
	inner.put(t, 0, rule)
	lookup()
	require.Len(t, match, 2, "equal-depth matches must not be selected by list order")
}

func TestMetadataPolicyOrganizedCreationAndInvalidExpressionReview(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	rule := models.MetadataPolicyRule{OnCreate: true, OnExisting: true, SkipOrganizedOnCreate: true, FilenameTitleFallback: true, Mappings: map[string]models.MetadataMapping{"title": {JQ: `"a", "b"`}}}
	policy := f.put(t, 0, rule)
	preview := policyPreview(t, f.repo, f.input(policy))
	require.Equal(t, "review", policyChange(t, preview, "title").Status, "do not hide a broken title mapping behind filename fallback")
	require.Empty(t, preview.AppliedFields())
	attachmentSQL(t, f.db, `UPDATE scenes SET organized=1 WHERE id=33`)
	require.Equal(t, "organized_at_creation", policyPreview(t, f.repo, f.input(policy)).State)
	input := f.input(policy)
	input.Created = false
	require.Equal(t, "ready", policyPreview(t, f.repo, input).State, "the migrated skip rule applies to creation, not every edit")
}

func TestMetadataPolicyPartialNameMatchesPreserveInheritedAttribution(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	first := archiveFind(t, f.repo, models.ArchivePerformer, 71)
	rule := models.MetadataPolicyRule{OnCreate: true, OnExisting: true, MarkOrganized: true, Mappings: map[string]models.MetadataMapping{
		"performers": {Value: json.RawMessage(`["` + first.UUID + `"]`)},
	}}
	policy := f.put(t, 0, rule)
	_, err := applyPolicy(f.repo, f.input(policy), "")
	require.NoError(t, err)
	attachmentSQL(t, f.db, `INSERT INTO performer_names(performer_id,name,position) VALUES(72,'Unique second',1)`)
	rule.Mappings["performers"] = models.MetadataMapping{Value: json.RawMessage(`["Unique second","Shared name"]`), PerformerNames: true}
	policy = f.put(t, policy.Revision, rule)
	applied, err := applyPolicy(f.repo, f.input(policy), "")
	require.NoError(t, err)
	require.Contains(t, applied.ReviewFields(), "performers")
	second := archiveFind(t, f.repo, models.ArchivePerformer, 72)
	var selected []string
	require.NoError(t, json.Unmarshal(metadataState(t, f.repo, f.entity.UUID, "performers").Value, &selected))
	require.ElementsMatch(t, []string{first.UUID, second.UUID}, selected)
}

func TestMetadataPolicyDisabledPreviewShowsProtectedAlternativesWithoutApplying(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	attachmentSQL(t, f.db, `UPDATE scenes SET title='Curated' WHERE id=33`)
	rule := models.MetadataPolicyRule{OnCreate: true, OnExisting: true, Mappings: map[string]models.MetadataMapping{"title": {JQ: `"Alternative"`}, "details": {Value: json.RawMessage(`"Draft"`)}}}
	policy := f.put(t, 0, rule)
	policy.Definition.Enabled = false
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		policy, err = f.repo.MetadataPolicy.Put(ctx, models.MetadataPolicyInput{CollectionUUID: f.collection.UUID, ExpectedCollectionRevision: f.collection.Revision, ExpectedRevision: policy.Revision, Origin: "review", Definition: policy.Definition})
		return err
	}))
	preview := policyPreview(t, f.repo, f.input(policy))
	require.Equal(t, "disabled", preview.State)
	choice := policyChange(t, preview, "title")
	require.Equal(t, "protected", choice.Status)
	require.Equal(t, `"Curated"`, string(choice.Current))
	require.Equal(t, `"Alternative"`, string(choice.Value))
	applied, err := applyPolicy(f.repo, f.input(policy), "")
	require.NoError(t, err)
	require.Equal(t, "disabled", applied.State)
	require.Empty(t, applied.AppliedFields())
	require.Equal(t, `""`, string(metadataState(t, f.repo, f.entity.UUID, "details").Value))
}
