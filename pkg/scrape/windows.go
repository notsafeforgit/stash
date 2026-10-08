// Package scrape defines the typed traversal contract used by source workers.
package scrape

import (
	"sort"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

const MaxWindows = 64
const TraversalBasis = "traversal"

func NormalizeWindow(w models.SourceWindow) (models.SourceWindow, error) {
	if (w.Basis != "" && w.Basis != TraversalBasis) || (w.Basis == TraversalBasis && w.Since != nil) {
		return w, models.ErrSourceRunInvalid
	}
	w.Until = w.Until.UTC()
	if w.Until.IsZero() || w.Until.Year() < 1 || w.Until.Year() > 9999 || w.Until.Nanosecond()%int(time.Millisecond) != 0 {
		return w, models.ErrSourceRunInvalid
	}
	if w.Since != nil {
		since := w.Since.UTC()
		if since.Year() < 1 || since.Year() > 9999 || !since.Before(w.Until) || since.Nanosecond()%int(time.Millisecond) != 0 {
			return w, models.ErrSourceRunInvalid
		}
		w.Since = &since
	}
	return w, nil
}

func earlier(a, b *time.Time) bool { return a == nil || (b != nil && a.Before(*b)) }

// Union preserves disjoint ranges; it never quietly scans the gap between two
// requests. Inputs have already been normalized at the admission boundary.
func Union(groups ...[]models.SourceWindow) []models.SourceWindow {
	all := make([]models.SourceWindow, 0)
	for _, group := range groups {
		all = append(all, group...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Basis != all[j].Basis {
			return all[i].Basis < all[j].Basis
		}
		if all[i].Since == nil {
			return all[j].Since != nil
		}
		return all[j].Since != nil && all[i].Since.Before(*all[j].Since)
	})
	ret := make([]models.SourceWindow, 0, len(all))
	for _, w := range all {
		if len(ret) == 0 || w.Basis != ret[len(ret)-1].Basis || (w.Since != nil && w.Since.After(ret[len(ret)-1].Until)) {
			ret = append(ret, w)
		} else if w.Until.After(ret[len(ret)-1].Until) {
			ret[len(ret)-1].Until = w.Until
		}
	}
	return ret
}

// Subtract returns only the unclaimed/uncompleted portions. In particular,
// extending a seven-day run to all history schedules its missing older range.
func Subtract(wanted, covered []models.SourceWindow) []models.SourceWindow {
	ret := Union(wanted)
	for _, c := range Union(covered) {
		next := make([]models.SourceWindow, 0, len(ret)+1)
		for _, w := range ret {
			if w.Basis != c.Basis {
				next = append(next, w)
				continue
			}
			if w.Basis == TraversalBasis {
				if w.Until.After(c.Until) {
					next = append(next, w)
				}
				continue
			}
			if (c.Since != nil && !w.Until.After(*c.Since)) || (w.Since != nil && !c.Until.After(*w.Since)) {
				next = append(next, w)
				continue
			}
			if c.Since != nil && earlier(w.Since, c.Since) {
				next = append(next, models.SourceWindow{Since: w.Since, Until: *c.Since})
			}
			if w.Until.After(c.Until) {
				since := c.Until
				next = append(next, models.SourceWindow{Since: &since, Until: w.Until})
			}
		}
		ret = next
	}
	return ret
}
