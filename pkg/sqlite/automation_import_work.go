package sqlite

import (
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"time"
)

type automationImportWork struct {
	snapshot *models.AutomationSnapshot
	manifest *scrape.AutomationSnapshotManifest
	captured time.Time
}

func (w *automationImportWork) decode(row catalogEvidenceRow) (*scrape.CatalogSnapshotRecord, error) {
	if scrape.CatalogSnapshotSHA([]byte(row.Data)) != row.SHA256 {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	return w.manifest.Record([]byte(row.Data))
}
