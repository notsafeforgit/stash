package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func policyImportInput(t *testing.T, f metadataPolicyFixture) models.MetadataPolicyImportInput {
	t.Helper()
	performer := archiveFind(t, f.repo, models.ArchivePerformer, 71)
	return models.MetadataPolicyImportInput{UUID: uuid.NewString(),
		Policy: models.MetadataPolicyInput{CollectionUUID: f.collection.UUID, ExpectedCollectionRevision: f.collection.Revision,
			Origin: "migration", Reason: "Reviewed retained folder default",
			Definition: models.MetadataPolicyDefinition{Enabled: true, ApplyToScans: true, Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{
				models.ArchiveScene: {OnCreate: true, FilenameTitleFallback: true, Mappings: map[string]models.MetadataMapping{
					"performers": {Value: json.RawMessage(`["` + performer.UUID + `"]`)}, "details": {Value: json.RawMessage(`"Private purchase notes"`)},
					"custom_fields": {Value: json.RawMessage(`{"z":"last","a":"first"}`)},
				}},
			}}},
		Document: models.MetadataPolicyImportDocument{Format: "legacy-metadata-policy", Version: 1, PluginVersion: "1.14.1",
			CapturedAt: "2026-09-29T00:00:00Z", SourceFiles: map[string]string{"config.py": strings.Repeat("a", 64)},
			Values: map[string]json.RawMessage{"settings/filename_title_fallback": json.RawMessage(`true`), "folder/actors": json.RawMessage(`["Private performer"]`)}},
		Dispositions: map[string]models.MetadataPolicyImportDisposition{
			"settings/filename_title_fallback": {Action: "mapped", Reason: "Native filename fallback"},
			"folder/actors":                    {Action: "mapped", Reason: "Explicitly selected portable performer UUID"},
		}, FolderSources: []models.MetadataPolicyImportDocumentRef{},
	}
}

func previewPolicyImport(t *testing.T, f metadataPolicyFixture, input models.MetadataPolicyImportInput) *models.MetadataPolicyImportPlan {
	t.Helper()
	var result *models.MetadataPolicyImportPlan
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.MetadataPolicyImport.Preview(ctx, input, time.Now())
		return err
	}))
	return result
}

func applyPolicyImport(f metadataPolicyFixture, input models.MetadataPolicyImportInput, expected string) (*models.MetadataPolicyImport, error) {
	var result *models.MetadataPolicyImport
	err := f.repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.MetadataPolicyImport.Apply(ctx, input, expected, time.Now())
		return err
	})
	return result, err
}

func TestMetadataPolicyImportReplayRetainsOriginalPolicyAndSources(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	input := policyImportInput(t, f)
	before := metadataHistory(t, f.repo, f.entity.UUID, "title")
	plan := previewPolicyImport(t, f, input)
	require.Nil(t, plan.PreviousPolicy)
	require.Len(t, plan.ReferenceRevisions, 1)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		policy, err := f.repo.MetadataPolicy.Find(ctx, f.collection.UUID)
		require.Nil(t, policy, "preview must not publish a policy")
		return err
	}))
	first, err := applyPolicyImport(f, input, plan.PlanSHA256)
	require.NoError(t, err)
	require.Equal(t, 1, first.PolicyRevision)
	require.Equal(t, before, metadataHistory(t, f.repo, f.entity.UUID, "title"), "policy import does not edit library items")
	// A newer policy cannot replace the original receipt after a lost response.
	f.put(t, 1, models.MetadataPolicyRule{OnExisting: true})
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	again, err := applyPolicyImport(f, input, plan.PlanSHA256)
	require.NoError(t, err)
	require.Equal(t, first, again)
	require.Equal(t, plan, previewPolicyImport(t, f, input))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		details, err := f.repo.MetadataPolicyImport.Find(ctx, input.UUID)
		require.NoError(t, err)
		original, marshalErr := json.Marshal(input)
		require.NoError(t, marshalErr)
		retained, marshalErr := json.Marshal(details.Binding)
		require.NoError(t, marshalErr)
		require.JSONEq(t, string(original), string(retained))
		list, err := f.repo.MetadataPolicyImport.List(ctx, f.collection.UUID, "", 1)
		require.NoError(t, err)
		require.Equal(t, []models.MetadataPolicyImport{*first}, list)
		list, err = f.repo.MetadataPolicyImport.List(ctx, f.collection.UUID, input.UUID, 1)
		require.Empty(t, list)
		return err
	}))
	input.Document.Values["folder/actors"] = json.RawMessage(`["Another private name"]`)
	_, err = applyPolicyImport(f, input, plan.PlanSHA256)
	require.ErrorIs(t, err, models.ErrMetadataPolicyImportConflict)
	// Private settings and source provenance must not leak through anonymisation.
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	raw := openRawDB(t, output)
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM metadata_policy_imports"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM metadata_policy_import_documents"))
}

func TestMetadataPolicyImportRejectsIncompleteReviewAndStaleReferences(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	input := policyImportInput(t, f)
	plan := previewPolicyImport(t, f, input)
	attachmentSQL(t, f.db, "UPDATE performer_names SET name='Renamed while reviewing' WHERE performer_id=71 AND position=0")
	_, err := applyPolicyImport(f, input, plan.PlanSHA256)
	require.ErrorIs(t, err, models.ErrMetadataPolicyImportConflict)
	review := previewPolicyImport(t, f, input)
	require.NotEqual(t, plan.PlanSHA256, review.PlanSHA256)
	input.Document.Values["settings/set_organized_only_if"] = json.RawMessage(`["title","cover_image"]`)
	_, err = applyPolicyImport(f, input, review.PlanSHA256)
	require.ErrorIs(t, err, models.ErrMetadataPolicyImportInvalid, "unclassified settings must not be lost")
	input.Dispositions["settings/set_organized_only_if"] = models.MetadataPolicyImportDisposition{Action: "review", Reason: "Cover completeness needs an explicit replacement"}
	_, err = applyPolicyImport(f, input, review.PlanSHA256)
	require.ErrorIs(t, err, models.ErrMetadataPolicyImportInvalid, "unresolved conversion must not activate")
	input.Policy.Definition.Enabled = false
	plan = previewPolicyImport(t, f, input)
	require.Equal(t, []string{"settings/set_organized_only_if"}, plan.ReviewKeys)
	_, err = applyPolicyImport(f, input, plan.PlanSHA256)
	require.NoError(t, err)
}

func TestMetadataPolicyImportGuardsDocumentSelectionsAndRollback(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	input := policyImportInput(t, f)
	doc := retainDocument(t, f.repo, models.SourceDocumentInput{Content: []byte("<movie><actor><name>Private performer</name></actor></movie>"), Encoding: "utf-8", Parser: "fixture", ParseStatus: "valid", Warnings: json.RawMessage(`[]`), Parsed: json.RawMessage(`{}`)})
	// Legacy folder documents may also retain an original catalog post identity.
	post := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "retained-folder-document"}, "")
	source := recordDocumentSource(t, f.repo, models.SourceDocumentSource{UUID: uuid.NewString(), DocumentUUID: doc.UUID, CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, RelativePath: "folder.nfo", PostUUID: &post.UUID, Origin: "migration", Details: json.RawMessage(`{}`)})
	var head *models.SourceDocumentHead
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		head, err = f.repo.SourceDocument.DecideHead(ctx, models.SourceDocumentHeadInput{CollectionUUID: f.collection.UUID, RelativePath: "folder.nfo", State: "linked", SourceUUID: source.UUID, Origin: "review", Reason: "Retain folder default"})
		return err
	}))
	for _, relativePath := range []string{"another-folder/folder.nfo", "item.nfo"} {
		unrelated := *source
		unrelated.UUID, unrelated.RelativePath = uuid.NewString(), relativePath
		recordDocumentSource(t, f.repo, unrelated)
		var unrelatedHead *models.SourceDocumentHead
		require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			var err error
			unrelatedHead, err = f.repo.SourceDocument.DecideHead(ctx, models.SourceDocumentHeadInput{CollectionUUID: f.collection.UUID, RelativePath: relativePath, State: "linked", SourceUUID: unrelated.UUID, Origin: "review"})
			return err
		}))
		input.FolderSources = []models.MetadataPolicyImportDocumentRef{{SourceUUID: unrelated.UUID, HeadUUID: unrelatedHead.UUID}}
		err := f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.MetadataPolicyImport.Preview(ctx, input, time.Now())
			return err
		})
		require.ErrorIs(t, err, models.ErrMetadataPolicyImportInvalid, "a retained post association does not establish folder scope")
	}
	input.FolderSources = []models.MetadataPolicyImportDocumentRef{{SourceUUID: source.UUID, HeadUUID: head.UUID}}
	plan := previewPolicyImport(t, f, input)
	rollback := errors.New("cancel transaction after policy publication")
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.MetadataPolicyImport.Apply(ctx, input, plan.PlanSHA256, time.Now())
		require.NoError(t, err)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	// A store failure after policy publication must also poison a transaction
	// whose caller accidentally ignores that error.
	attachmentSQL(t, f.db, `CREATE TRIGGER fixture_policy_import_failure BEFORE INSERT ON metadata_policy_imports
BEGIN SELECT RAISE(ABORT,'fixture receipt failure'); END`)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, applyErr := f.repo.MetadataPolicyImport.Apply(ctx, input, plan.PlanSHA256, time.Now())
		require.Error(t, applyErr)
		return nil
	})
	require.Error(t, err)
	attachmentSQL(t, f.db, `DROP TRIGGER fixture_policy_import_failure`)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		policy, err := f.repo.MetadataPolicy.Find(ctx, f.collection.UUID)
		require.NoError(t, err)
		require.Nil(t, policy)
		receipt, err := f.repo.MetadataPolicyImport.Find(ctx, input.UUID)
		require.Nil(t, receipt)
		return err
	}))
	_, err = applyPolicyImport(f, input, plan.PlanSHA256)
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		retained, err := f.repo.SourceDocument.Source(ctx, source.UUID)
		require.NoError(t, err)
		require.Equal(t, source, retained, "policy import must preserve historical post provenance")
		receipt, err := f.repo.MetadataPolicyImport.Find(ctx, input.UUID)
		require.NoError(t, err)
		require.Equal(t, input.FolderSources, receipt.FolderSources)
		return nil
	}))
	input.UUID = uuid.NewString()
	input.Policy.ExpectedRevision = 1
	plan = previewPolicyImport(t, f, input)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceDocument.DecideHead(ctx, models.SourceDocumentHeadInput{CollectionUUID: f.collection.UUID, RelativePath: "folder.nfo", ExpectedRevision: head.Revision, State: "unlinked", Origin: "review", Reason: "Default no longer selected"})
		return err
	}))
	_, err = applyPolicyImport(f, input, plan.PlanSHA256)
	require.ErrorIs(t, err, models.ErrMetadataPolicyImportConflict)
}
