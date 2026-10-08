package identify

import (
	"context"

	"github.com/stashapp/stash/pkg/models"
)

// Relationship unit fixtures use in-memory mocks. The external-package
// integration tests exercise the real recorder with native SQLite.
func testMetadataRecorder(context.Context, models.ArchiveEntityKind, int, string, *string, []string) error {
	return nil
}
