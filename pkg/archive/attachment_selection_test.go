package archive_test

import (
	"testing"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestAttachmentSelectionCombinesPartialKnowledgeAndPrunesRedundantEvidence(t *testing.T) {
	count := 3
	inputs := []models.SourceAttachmentManifestInput{
		{ExpectedCount: &count, Entries: []models.SourceAttachmentEntry{manifestEntry(0, "first"), manifestEntry(2, "repeated")}},
		{Entries: []models.SourceAttachmentEntry{manifestEntry(1, "middle"), manifestEntry(2, "repeated")}},
	}
	merged, err := archive.MergeAttachmentManifests(inputs)
	require.NoError(t, err)
	require.Empty(t, merged.Conflicts)
	require.Equal(t, []int{0, 1}, merged.Contributors)
	require.Len(t, merged.Manifest.Entries, 3)
	require.Equal(t, 3, *merged.Manifest.ExpectedCount)
	require.False(t, merged.Manifest.Complete, "covering all positions across captures is not proof of a single complete observation")
	full := merged.Manifest
	full.Complete = true
	inputs = append(inputs, full)
	merged, err = archive.MergeAttachmentManifests(inputs)
	require.NoError(t, err)
	require.True(t, merged.Manifest.Complete)
	require.Equal(t, []int{2}, merged.Contributors, "the complete source list subsumes partial contributors")
	inputs = []models.SourceAttachmentManifestInput{full, full}
	merged, err = archive.MergeAttachmentManifests(inputs)
	require.NoError(t, err)
	require.Equal(t, []int{0}, merged.Contributors, "identical later captures cannot churn selected evidence")
	full.Entries = nil
	full.ExpectedCount = nil
	merged, err = archive.MergeAttachmentManifests([]models.SourceAttachmentManifestInput{full, full})
	require.NoError(t, err)
	require.Equal(t, []int{0}, merged.Contributors)
}

func TestAttachmentSelectionConflictsNeverProduceAUsableList(t *testing.T) {
	one, two := 1, 2
	unknown := manifestEntry(0, "first")
	unknown.MediaKind = "unknown"
	video := manifestEntry(0, "first")
	video.MediaKind = "video"
	for _, tc := range []struct {
		kind        string
		left, right models.SourceAttachmentManifestInput
	}{
		{"count", models.SourceAttachmentManifestInput{ExpectedCount: &one}, models.SourceAttachmentManifestInput{ExpectedCount: &two}},
		{"position", models.SourceAttachmentManifestInput{Entries: []models.SourceAttachmentEntry{manifestEntry(0, "first")}}, models.SourceAttachmentManifestInput{Entries: []models.SourceAttachmentEntry{manifestEntry(0, "other")}}},
		{"media-kind", models.SourceAttachmentManifestInput{Entries: []models.SourceAttachmentEntry{manifestEntry(0, "first")}}, models.SourceAttachmentManifestInput{Entries: []models.SourceAttachmentEntry{video}}},
		{"count-position", models.SourceAttachmentManifestInput{ExpectedCount: &one}, models.SourceAttachmentManifestInput{Entries: []models.SourceAttachmentEntry{manifestEntry(1, "outside")}}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			merged, err := archive.MergeAttachmentManifests([]models.SourceAttachmentManifestInput{tc.left, tc.right})
			require.NoError(t, err)
			require.Equal(t, tc.kind, merged.Conflicts[0].Kind)
			require.Empty(t, merged.Contributors)
			require.Empty(t, merged.Manifest.Entries)
		})
	}
	merged, err := archive.MergeAttachmentManifests([]models.SourceAttachmentManifestInput{
		{Entries: []models.SourceAttachmentEntry{unknown}}, {Entries: []models.SourceAttachmentEntry{video}},
	})
	require.NoError(t, err)
	require.Empty(t, merged.Conflicts)
	require.Equal(t, "video", merged.Manifest.Entries[0].MediaKind)
	require.Equal(t, []int{1}, merged.Contributors)
}

func TestAttachmentSelectionBoundsAndRetainsIndependentAlbumFacts(t *testing.T) {
	count := 2
	inputs := []models.SourceAttachmentManifestInput{
		{DeclaredAlbum: true}, {ExpectedCount: &count}, {Entries: []models.SourceAttachmentEntry{manifestEntry(0, "first")}},
	}
	merged, err := archive.MergeAttachmentManifests(inputs)
	require.NoError(t, err)
	require.Equal(t, []int{0, 1, 2}, merged.Contributors)
	require.True(t, merged.Manifest.DeclaredAlbum)
	_, err = archive.MergeAttachmentManifests(nil)
	require.Error(t, err)
	_, err = archive.MergeAttachmentManifests(make([]models.SourceAttachmentManifestInput, archive.MaxSelectionManifests+2))
	require.Error(t, err)
	input := models.SourceAttachmentManifestInput{}
	for i := 0; i < archive.MaxManifestEntries; i++ {
		input.Entries = append(input.Entries, manifestEntry(i, "repeated"))
	}
	_, err = archive.MergeAttachmentManifests([]models.SourceAttachmentManifestInput{input, input, input, input, input})
	require.ErrorContains(t, err, "source-entry budget")
}
