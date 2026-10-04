package sqlite

import (
	"encoding/json"
	"reflect"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func verifyDiscoveryListingLegacy(get enrichmentGet, input models.DiscoveryListingInput) error {
	if input.RecoveryOf != nil {
		_, err := discoveryRecoveryOriginal(get, input)
		return err
	}
	if input.Legacy == nil {
		return nil
	}
	var row struct {
		Data      string    `db:"data"`
		Digest    string    `db:"data_sha256"`
		Captured  string    `db:"captured_at"`
		Account   string    `db:"account_uuid"`
		NotBefore time.Time `db:"not_before"`
	}
	err := get(&row, `SELECT e.data,e.data_sha256,s.captured_at,r.account_uuid,r.not_before
 FROM automation_discovery_records r JOIN automation_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 JOIN automation_snapshots s ON s.uuid=r.snapshot_uuid JOIN automation_discovery_imports i ON i.snapshot_uuid=r.snapshot_uuid
 WHERE r.snapshot_uuid=? AND r.ordinal=? AND e.source_table='discovery_accounts' AND r.outcome='mapped' AND r.disposition='held'
 AND r.phase='listing' AND i.state!='running'`, input.Legacy.SnapshotUUID, input.Legacy.AccountOrdinal)
	if err != nil {
		return models.ErrDiscoveryConflict
	}
	if row.Digest != sourceDigest([]byte(row.Data)) || row.Account != input.AccountUUID || input.NotBefore.Before(row.NotBefore) {
		return models.ErrDiscoveryConflict
	}
	tree, err := archive.DecodeJSONObject([]byte(row.Data), scrape.CatalogChunkLimit)
	if err != nil {
		return err
	}
	values, ok := tree["values"].(map[string]any)
	if !ok {
		return models.ErrSourcePayloadCorrupt
	}
	captured, err := time.Parse(time.RFC3339Nano, row.Captured)
	if err != nil {
		return err
	}
	prepared, reason := scrape.PrepareAutomationDiscovery("discovery_accounts", values, captured)
	if reason != "" || prepared == nil {
		return models.ErrSourcePayloadCorrupt
	}
	p := prepared.Projection
	if p.ProfileURL != input.ProfileURL || p.HistoricalPages == nil || *p.HistoricalPages != input.HistoricalPages ||
		p.NotBefore == nil || input.NotBefore.Before(*p.NotBefore) || p.StagedKind != "" {
		return models.ErrDiscoveryConflict
	}
	var cursor map[string]string
	if raw, ok := values["cursor_json"].(string); ok {
		if err := json.Unmarshal([]byte(raw), &cursor); err != nil {
			return models.ErrDiscoveryConflict
		}
	}
	if !reflect.DeepEqual(cursor, input.InitialCursor) {
		return models.ErrDiscoveryConflict
	}
	var targets struct {
		Count int `db:"count"`
		Wrong int `db:"wrong"`
	}
	if err := get(&targets, `SELECT count(*) AS count,coalesce(sum(collection_uuid IS NOT ? OR outcome!='mapped'),0) AS wrong
 FROM automation_discovery_records WHERE snapshot_uuid=? AND account_ordinal=?`, input.CollectionUUID, input.Legacy.SnapshotUUID, input.Legacy.AccountOrdinal); err != nil {
		return err
	}
	if targets.Count == 0 || targets.Wrong != 0 {
		return models.ErrDiscoveryConflict
	}
	return nil
}
