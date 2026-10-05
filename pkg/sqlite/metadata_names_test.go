package sqlite_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func metadataNames(t *testing.T, repo models.Repository, kind models.ArchiveEntityKind, name string) []models.MetadataNameCandidate {
	t.Helper()
	var ret []models.MetadataNameCandidate
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.MetadataField.NameCandidates(ctx, kind, name)
		return err
	}))
	return ret
}

func metadataNamedEntities(t *testing.T, f metadataPolicyFixture) {
	t.Helper()
	attachmentSQL(t, f.db, `INSERT INTO studios(id,name,created_at,updated_at) VALUES
 (201,'Studio canonical',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP), (202,'Collision',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO studio_aliases(studio_id,alias) VALUES(201,'Studio alias'),(201,'STUDIO ALIAS'),(201,'collision');
 INSERT INTO tags(id,name,created_at,updated_at) VALUES
 (301,'Tag canonical',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP), (302,'Collision',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO tag_aliases(tag_id,alias) VALUES(301,'Tag alias'),(301,'TAG ALIAS'),(301,'collision');
 INSERT INTO groups(id,name,created_at,updated_at) VALUES
 (401,'Album',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),(402,'Repeated album',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
 (403,'Repeated album',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);`)
}

func TestMetadataNameCandidatesShareCanonicalAliasesAndCollisionRules(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	metadataNamedEntities(t, f)
	for _, tc := range []struct {
		kind models.ArchiveEntityKind
		name string
		ids  []int
	}{
		{models.ArchivePerformer, "sHaReD nAmE", []int{71, 72}},
		{models.ArchiveStudio, "studio canonical", []int{201}},
		{models.ArchiveStudio, "STudio ALias", []int{201}},
		{models.ArchiveStudio, "collision", []int{201, 202}},
		{models.ArchiveTag, "tag canonical", []int{301}},
		{models.ArchiveTag, "tAg aLiAs", []int{301}},
		{models.ArchiveTag, "Collision", []int{301, 302}},
		{models.ArchiveGroup, "album", []int{401}},
		{models.ArchiveGroup, "repeated album", []int{402, 403}},
		{models.ArchiveStudio, "Studio aliaz", nil},
	} {
		matches := metadataNames(t, f.repo, tc.kind, tc.name)
		ids := []int{}
		for i, match := range matches {
			ids = append(ids, match.LocalID)
			require.Positive(t, match.Revision)
			if i > 0 {
				require.Less(t, matches[i-1].UUID, match.UUID)
			}
		}
		require.ElementsMatch(t, tc.ids, ids, "%s %s", tc.kind, tc.name)
	}
	// More than 100 spellings of one entity must not hide another candidate.
	for variant := range 128 {
		name := []byte("abcdefg")
		for i := range name {
			if variant&(1<<i) != 0 {
				name[i] -= 'a' - 'A'
			}
		}
		attachmentSQL(t, f.db, "INSERT INTO studio_aliases(studio_id,alias) VALUES(201,?)", string(name))
	}
	attachmentSQL(t, f.db, "UPDATE studios SET name='ABCDEFG' WHERE id=202")
	require.Len(t, metadataNames(t, f.repo, models.ArchiveStudio, "abcdefg"), 2)
	for i := range 110 {
		attachmentSQL(t, f.db, "INSERT INTO groups(id,name,created_at,updated_at) VALUES(?,'Many matches',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)", 1000+i)
	}
	require.Len(t, metadataNames(t, f.repo, models.ArchiveGroup, "many matches"), 101, "the extra candidate reports overflow")
	for _, query := range []struct{ table, column, index string }{
		{"studios", "name", "metadata_studio_names"}, {"studio_aliases", "alias", "metadata_studio_aliases"},
		{"tags", "name", "metadata_tag_names"}, {"tag_aliases", "alias", "metadata_tag_aliases"},
		{"groups", "name", "metadata_group_names"},
	} {
		raw := openRawDB(t, f.db.DatabasePath())
		var id, parent, unused int
		var plan string
		require.NoError(t, raw.QueryRow("EXPLAIN QUERY PLAN SELECT * FROM "+query.table+" INDEXED BY "+query.index+" WHERE "+query.column+"=? COLLATE NOCASE", "example").Scan(&id, &parent, &unused, &plan))
		require.Contains(t, plan, "SEARCH")
		require.Contains(t, plan, query.index)
		require.NoError(t, raw.Close())
	}
}

func TestMetadataPolicyReferenceNamesApplyAndRecheckAliases(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	metadataNamedEntities(t, f)
	rule := models.MetadataPolicyRule{OnCreate: true, OnExisting: true, MarkOrganized: true, Mappings: map[string]models.MetadataMapping{
		"studio": {Value: json.RawMessage(`"studio ALIAS"`), ReferenceNames: true},
		"tags":   {Value: json.RawMessage(`["Tag alias","Tag alias"]`), ReferenceNames: true},
		"groups": {Value: json.RawMessage(`[{"name":"album","scene_index":4},{"name":"album","scene_index":4}]`), ReferenceNames: true},
	}}
	policy := f.put(t, 0, rule)
	input := f.input(policy)
	preview := policyPreview(t, f.repo, input)
	for _, field := range []string{"studio", "tags", "groups"} {
		change := policyChange(t, preview, field)
		require.Equal(t, "ready", change.Status)
		require.Len(t, change.Names, 1, "repeated input names share a single candidate result")
		require.Equal(t, "matched", change.Names[0].Status)
		require.Len(t, change.ReferenceRevisions, 1)
	}
	attachmentSQL(t, f.db, "UPDATE studios SET name='studio alias' WHERE id=202")
	_, err := applyPolicy(f.repo, input, preview.Digest)
	require.ErrorIs(t, err, models.ErrMetadataPolicyConflict, "a canonical/alias collision must invalidate an earlier preview")
	conflict := policyPreview(t, f.repo, input)
	require.Equal(t, "ambiguous", policyChange(t, conflict, "studio").Names[0].Status)
	require.Equal(t, "review", policyChange(t, conflict, "studio").Status)
	attachmentSQL(t, f.db, "UPDATE studios SET name='Separate studio' WHERE id=202")
	preview = policyPreview(t, f.repo, input)
	applied, err := applyPolicy(f.repo, input, preview.Digest)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"studio", "tags", "groups", "organized"}, applied.AppliedFields())
	group := archiveFind(t, f.repo, models.ArchiveGroup, 401)
	require.JSONEq(t, fmt.Sprintf(`[{"uuid":%q,"scene_index":4}]`, group.UUID), string(metadataState(t, f.repo, f.entity.UUID, "groups").Value))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	replayed, err := applyPolicy(f.repo, input, "")
	require.NoError(t, err)
	require.Empty(t, replayed.AppliedFields())
}

func TestMetadataPolicyPartialNameMatchesPreserveExistingRelations(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	metadataNamedEntities(t, f)
	rule := models.MetadataPolicyRule{OnCreate: true, OnExisting: true, Mappings: map[string]models.MetadataMapping{
		"tags": {Value: json.RawMessage(`["Collision"]`), ReferenceNames: true},
	}}
	// First select a fixed inherited tag and group; a later partial name result
	// must keep them while adding its unambiguous candidates.
	tag := archiveFind(t, f.repo, models.ArchiveTag, 302)
	group := archiveFind(t, f.repo, models.ArchiveGroup, 402)
	rule.Mappings["tags"] = models.MetadataMapping{Value: json.RawMessage(fmt.Sprintf(`[%q]`, tag.UUID))}
	rule.Mappings["groups"] = models.MetadataMapping{Value: json.RawMessage(fmt.Sprintf(`[{"uuid":%q,"scene_index":2}]`, group.UUID))}
	policy := f.put(t, 0, rule)
	_, err := applyPolicy(f.repo, f.input(policy), "")
	require.NoError(t, err)
	rule.MarkOrganized = true
	rule.Mappings["tags"] = models.MetadataMapping{Value: json.RawMessage(`["Tag alias","Collision"]`), ReferenceNames: true}
	rule.Mappings["groups"] = models.MetadataMapping{Value: json.RawMessage(`[{"name":"Album","scene_index":7},{"name":"Repeated album"}]`), ReferenceNames: true}
	policy = f.put(t, policy.Revision, rule)
	preview := policyPreview(t, f.repo, f.input(policy))
	for _, field := range []string{"tags", "groups"} {
		change := policyChange(t, preview, field)
		require.Equal(t, "ready", change.Status)
		require.Len(t, change.ReferenceRevisions, 2)
		require.Equal(t, "ambiguous", change.Names[1].Status)
	}
	for _, change := range preview.Changes {
		require.NotEqual(t, "organized", change.Field)
	}
	_, err = applyPolicy(f.repo, f.input(policy), preview.Digest)
	require.NoError(t, err)
	require.Contains(t, string(metadataState(t, f.repo, f.entity.UUID, "tags").Value), tag.UUID)
	require.Contains(t, string(metadataState(t, f.repo, f.entity.UUID, "groups").Value), group.UUID)
	require.Contains(t, string(metadataState(t, f.repo, f.entity.UUID, "groups").Value), `"scene_index":7`)
}
