package archive_test

import (
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func manifestEntry(position int, id string) models.SourceAttachmentEntry {
	return models.SourceAttachmentEntry{Position: position, Reference: models.SourcePostIdentifier{Namespace: "native:reddit", Value: id}, MediaKind: "image"}
}

func TestAttachmentManifestSourceOrderCompletenessAndRepeatedMedia(t *testing.T) {
	count := 5
	input := models.SourceAttachmentManifestInput{ExpectedCount: &count, Entries: []models.SourceAttachmentEntry{manifestEntry(4, "OpaqueID"), manifestEntry(0, "OpaqueID")}}
	out, err := archive.NormalizeAttachmentManifest(input)
	require.NoError(t, err)
	require.Equal(t, 0, out.Entries[0].Position)
	require.Equal(t, "OpaqueID", out.Entries[0].Reference.Value)
	require.Equal(t, 4, input.Entries[0].Position, "normalization must not reorder caller-owned data")
	*out.ExpectedCount = 6
	require.Equal(t, 5, count)
	input.Complete = true
	_, err = archive.NormalizeAttachmentManifest(input)
	require.Error(t, err, "partial source positions cannot become complete just because all local downloads finished")
	input.ExpectedCount = nil
	input.Entries[0].Position = 1
	out, err = archive.NormalizeAttachmentManifest(input)
	require.NoError(t, err)
	require.Equal(t, 2, *out.ExpectedCount, "the same source attachment may occupy multiple positions")
	for _, manifest := range []models.SourceAttachmentManifest{{DeclaredAlbum: true}, {EntryCount: 2}, {ExpectedCount: &count}} {
		require.True(t, manifest.IsAlbum())
	}
	require.False(t, (models.SourceAttachmentManifest{EntryCount: 1}).IsAlbum())
}

func TestAttachmentManifestRejectsInvalidAndOversizedInputs(t *testing.T) {
	negative, one := -1, 1
	badNamespace := manifestEntry(0, "value")
	badNamespace.Reference.Namespace = "reddit"
	badKind := manifestEntry(0, "value")
	badKind.MediaKind = "audio"
	for _, input := range []models.SourceAttachmentManifestInput{
		{ExpectedCount: &negative},
		{Entries: []models.SourceAttachmentEntry{manifestEntry(-1, "a")}},
		{Entries: []models.SourceAttachmentEntry{manifestEntry(archive.MaxManifestPositions, "a")}},
		{Entries: []models.SourceAttachmentEntry{manifestEntry(0, "a"), manifestEntry(0, "b")}},
		{Entries: []models.SourceAttachmentEntry{manifestEntry(0, "")}},
		{Entries: []models.SourceAttachmentEntry{badNamespace}},
		{Entries: []models.SourceAttachmentEntry{badKind}},
		{ExpectedCount: &one, Entries: []models.SourceAttachmentEntry{manifestEntry(1, "a")}},
		{Complete: true, ExpectedCount: &one},
		{Entries: make([]models.SourceAttachmentEntry, archive.MaxManifestEntries+1)},
	} {
		_, err := archive.NormalizeAttachmentManifest(input)
		require.Error(t, err)
	}
	input := models.SourceAttachmentManifestInput{Complete: true}
	for i := 0; i < archive.MaxManifestEntries; i++ {
		input.Entries = append(input.Entries, manifestEntry(i, strings.Repeat("a", 2048)))
	}
	_, err := archive.NormalizeAttachmentManifest(input)
	require.ErrorContains(t, err, "4 MiB")
	input.Entries = nil
	out, err := archive.NormalizeAttachmentManifest(input)
	require.NoError(t, err)
	require.Zero(t, *out.ExpectedCount, "a confirmed empty list differs from an unknown list")
}
