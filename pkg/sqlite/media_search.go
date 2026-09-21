package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

// Bound both the probe and its ID set. Common terms stop early and keep the
// original plan; selective searches reuse one probe for the count and the page.
const mediaSearchCandidateLimit = 4096

// prefilterMediaSearch only narrows the candidate entities. Keep every original
// join and predicate: terms must still match the same file/fingerprint/marker
// row, and aggregate fields must still include precisely the matching files.
// Existing entity criteria may already be selective, so leave those plans alone.
func (qb *queryBuilder) prefilterMediaSearch(ctx context.Context, table string, find *models.FindFilterType, filter *filterBuilder) error {
	if find == nil || find.Q == nil || !filter.empty() {
		return nil
	}
	if _, err := getTx(ctx); err != nil {
		return nil // The probe and result must observe the same database snapshot.
	}
	specs := models.ParseSearchString(*find.Q)
	var terms []string
	if len(specs.MustHave) > 0 {
		terms = specs.MustHave[:1]
	} else if len(specs.AnySets) > 0 {
		terms = specs.AnySets[0]
	}
	if len(terms) == 0 {
		return nil // Negative-only searches have no necessary positive candidate set.
	}

	sql, args := mediaSearchCandidates(table, terms)
	ids, err := qb.repository.runIdsQuery(ctx, sql, args)
	if err != nil {
		return fmt.Errorf("finding media search candidates: %w", err)
	}
	if len(ids) > mediaSearchCandidateLimit {
		return nil
	}
	if len(ids) == 0 {
		qb.addWhere("0")
		return nil
	}

	// One JSON parameter avoids consuming SQLite's variable budget when the
	// original search has many terms. IDs are integers read in this transaction.
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	qb.addWhere(table + ".id IN (SELECT value FROM json_each(?))")
	qb.addArg(string(encoded))
	return nil
}

func mediaSearchCandidates(table string, terms []string) (string, []interface{}) {
	id := imageIDColumn
	if table == sceneTable {
		id = sceneIDColumn
	}
	var args []interface{}
	match := func(columns ...string) string {
		var clauses []string
		for _, column := range columns {
			for _, term := range terms {
				clauses = append(clauses, column+" LIKE ?")
				args = append(args, like(term))
			}
		}
		return "(" + strings.Join(clauses, " OR ") + ")"
	}
	parts := []string{
		"SELECT id FROM " + table + " WHERE " + match("title", "details"),
		"SELECT " + id + " AS id FROM " + table + "_files JOIN files ON file_id = files.id JOIN folders ON files.parent_folder_id = folders.id WHERE " + match("folders.path || '"+string(filepath.Separator)+"' || files.basename"),
		"SELECT " + id + " AS id FROM " + table + "_files JOIN files_fingerprints USING (file_id) WHERE " + match("fingerprint"),
	}
	if table == sceneTable {
		parts = append(parts, "SELECT scene_id AS id FROM scene_markers WHERE "+match("title"))
	}
	// DISTINCT outside UNION ALL allows LIMIT to stop the source scans as soon
	// as enough distinct entities are found, even for entities with many files.
	return fmt.Sprintf("SELECT DISTINCT id FROM (%s) LIMIT %d", strings.Join(parts, " UNION ALL "), mediaSearchCandidateLimit+1), args
}
