package sqlite

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

// NameCandidates shares one exact, indexed resolver between retained-edit
// review and intake policies. Multiple spellings of one entity count once;
// canonical-name/alias collisions across entities remain separate candidates.
func (s *MetadataFieldStore) NameCandidates(ctx context.Context, kind models.ArchiveEntityKind, name string) ([]models.MetadataNameCandidate, error) {
	if !validAccountText(name, 1024, false) {
		return nil, fmt.Errorf("invalid metadata reference name")
	}
	ret := []models.MetadataNameCandidate{}
	if kind == models.ArchivePerformer {
		err := dbWrapper.Select(ctx, &ret, `SELECT DISTINCT e.uuid,performers.id AS local_id,e.revision,
 `+performerPrimaryNameSQL+` AS name,coalesce(performers.disambiguation,'') AS disambiguation
 FROM performer_names n INDEXED BY performer_names_lookup
 JOIN performers ON performers.id=n.performer_id
 JOIN archive_entities e ON e.performer_id=performers.id AND e.state='active'
 WHERE n.name=? COLLATE NOCASE ORDER BY e.uuid LIMIT 101`, name)
		return ret, err
	}
	table, column, index := "", "", ""
	switch kind {
	case models.ArchiveTag:
		table, column, index = "tags", "tag_id", "metadata_tag_names"
	case models.ArchiveStudio:
		table, column, index = "studios", "studio_id", "metadata_studio_names"
	case models.ArchiveGroup:
		table, column, index = "groups", "group_id", "metadata_group_names"
	default:
		return nil, fmt.Errorf("unsupported metadata reference kind %q", kind)
	}
	columns := `e.uuid,t.id AS local_id,e.revision,t.name,'' AS disambiguation`
	canonical := `SELECT ` + columns + ` FROM ` + table + ` t INDEXED BY ` + index + `
 JOIN archive_entities e ON e.` + column + `=t.id AND e.state='active'
 WHERE t.name=? COLLATE NOCASE ORDER BY e.uuid LIMIT 101`
	if kind == models.ArchiveGroup {
		// Group aliases are free-form text, not normalized individual names.
		err := dbWrapper.Select(ctx, &ret, canonical, name)
		return ret, err
	}
	aliases, aliasIndex := "tag_aliases", "metadata_tag_aliases"
	if kind == models.ArchiveStudio {
		aliases, aliasIndex = "studio_aliases", "metadata_studio_aliases"
	}
	// Bound each branch after deduplication. Limiting raw alias rows first
	// could hide a second entity behind many case variants of the first.
	query := `SELECT * FROM (` + canonical + `) UNION SELECT * FROM (
 SELECT DISTINCT ` + columns + ` FROM ` + aliases + ` a INDEXED BY ` + aliasIndex + `
 JOIN ` + table + ` t ON t.id=a.` + column + `
 JOIN archive_entities e ON e.` + column + `=t.id AND e.state='active'
 WHERE a.alias=? COLLATE NOCASE ORDER BY e.uuid LIMIT 101)
 ORDER BY uuid LIMIT 101`
	err := dbWrapper.Select(ctx, &ret, query, name, name)
	return ret, err
}
