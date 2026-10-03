package archive

import (
	"slices"
	"time"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/models"
)

func validEnrichmentObservedTime(value string) bool {
	if !enrichmentObservedTime.MatchString(value) {
		return false
	}
	observed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && !observed.IsZero() && observed.UTC().Year() >= 1 && observed.UTC().Year() <= 9999
}

// A retained capture already contains its original parent context. Preserve both
// keys and count their depth rather than pretending this body is a fresh root.
func retainedContextDepth(data sourceObject) (int, error) {
	path, err := retainedContextAncestors(data, 0)
	return len(path) - 1, err
}

// A direct ancestor shortcut may repeat the end of a longer chain. Competing
// category chains cannot safely select gallery-dl's inherited source settings.
func retainedContextAncestors(data sourceObject, depth int) ([]string, error) {
	category, ok := data["category"].(string)
	if !ok || category == "" || utf8.RuneCountInString(category) > 128 || depth > 2 {
		return nil, models.ErrEnrichmentInvalid
	}
	var longest []string
	for _, key := range []string{"_parent", "_reddit"} {
		value, exists := data[key]
		if !exists || value == nil {
			continue
		}
		parent, ok := value.(sourceObject)
		if !ok {
			return nil, models.ErrEnrichmentInvalid
		}
		path, err := retainedContextAncestors(parent, depth+1)
		if err != nil {
			return nil, err
		}
		if len(path) > len(longest) {
			path, longest = longest, path
		}
		if !slices.Equal(path, longest[len(longest)-len(path):]) {
			return nil, models.ErrEnrichmentInvalid
		}
	}
	return append([]string{category}, longest...), nil
}
