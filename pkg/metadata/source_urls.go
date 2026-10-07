package metadata

import (
	"context"
	"encoding/json"
	"sort"
)

const maxPolicySourceURLs = 4096
const maxPolicySourceURLBytes = 1 << 20

// URLs are shared post evidence, not necessarily repeated in the capture's raw
// payload. Read the chosen post's current identity group in bounded pages, with
// one value per exact URL. An oversized set is null,
// never a partial replacement that could silently erase selected entity URLs.
func (s Service) sourcePostURLs(ctx context.Context, post string) ([]string, bool, error) {
	values := []string{}
	after, size := "", 2
	for {
		rows, err := s.Repo.SourcePostLinks.CurrentURLs(ctx, post, after, 100)
		if err != nil {
			return nil, false, err
		}
		for _, row := range rows {
			encoded, err := json.Marshal(row.URL)
			if err != nil {
				return nil, false, err
			}
			size += len(encoded) + 1
			if len(values) == maxPolicySourceURLs || size > maxPolicySourceURLBytes {
				return nil, false, nil
			}
			values = append(values, row.URL)
		}
		if len(rows) < 100 {
			sort.Strings(values)
			return values, true, nil
		}
		after = rows[len(rows)-1].UUID
	}
}
