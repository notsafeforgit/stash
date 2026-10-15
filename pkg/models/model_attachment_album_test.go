package models_test

import (
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourceAlbumCardinality(t *testing.T) {
	zero, one, three := 0, 1, 3
	for _, test := range []struct {
		name     string
		manifest models.SourceAttachmentManifest
		album    bool
	}{
		{"ordinary single attachment", models.SourceAttachmentManifest{Complete: true, EntryCount: 1}, false},
		{"declared complete single attachment", models.SourceAttachmentManifest{DeclaredAlbum: true, Complete: true, EntryCount: 1}, false},
		{"declared total of one", models.SourceAttachmentManifest{DeclaredAlbum: true, ExpectedCount: &one, EntryCount: 1}, false},
		{"known single attachment not yet captured", models.SourceAttachmentManifest{DeclaredAlbum: true, ExpectedCount: &one}, false},
		{"confirmed empty album", models.SourceAttachmentManifest{DeclaredAlbum: true, Complete: true, ExpectedCount: &zero}, false},
		{"declared unknown album", models.SourceAttachmentManifest{DeclaredAlbum: true}, true},
		{"one captured of unknown total", models.SourceAttachmentManifest{DeclaredAlbum: true, EntryCount: 1}, true},
		{"one captured of three", models.SourceAttachmentManifest{ExpectedCount: &three, EntryCount: 1}, true},
		{"multiple attachments without album flag", models.SourceAttachmentManifest{Complete: true, EntryCount: 2}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.album, test.manifest.IsAlbum())
			selection := models.AttachmentSelection{
				Decision:      models.AttachmentSelectionDecision{Mode: "automatic"},
				DeclaredAlbum: test.manifest.DeclaredAlbum, Complete: test.manifest.Complete,
				ExpectedCount: test.manifest.ExpectedCount,
				Entries:       make([]models.SourceAttachmentManifestEntry, test.manifest.EntryCount),
			}
			require.Equal(t, test.album, selection.IsAlbum())
			selection.Decision.Mode = "disabled"
			require.False(t, selection.IsAlbum())
		})
	}
}
