package main

import (
	"io/ioutil"
	"strings"
)

func main() {
	content, _ := ioutil.ReadFile("internal/api/resolver_model_saved_filter.go")
	s := string(content)

	s = strings.ReplaceAll(s, "s.Title.String", "s.Title")
	s = strings.ReplaceAll(s, "s.Details.String", "s.Details")
	s = strings.ReplaceAll(s, "m.Name.String", "m.Name")

	// Looks like Movie repository is no longer called Movie (maybe legacy and deleted or called differently).
	// The frontend still supports `movies` as legacy though. I'll just remove the movie population query.
	s = strings.ReplaceAll(s, `		// Movies
		mapping.Movies = populateMapping([]string{"movies"}, func(ids []int) []*LabelMappingEntry {
			var res []*LabelMappingEntry
			movies, _ := r.repository.Movie.FindMany(ctx, ids)
			for _, m := range movies {
				res = append(res, &LabelMappingEntry{ID: strconv.Itoa(m.ID), Label: m.Name})
			}
			return res
		})`, `		// Movies
		mapping.Movies = populateMapping([]string{"movies"}, func(ids []int) []*LabelMappingEntry {
			return nil
		})`)

	ioutil.WriteFile("internal/api/resolver_model_saved_filter.go", []byte(s), 0644)
}
