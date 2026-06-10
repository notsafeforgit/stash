package models

import (
	"fmt"
	"io"
	"strconv"
)

type FilterMode string

const (
	FilterModeScenes       FilterMode = "SCENES"
	FilterModePerformers   FilterMode = "PERFORMERS"
	FilterModeStudios      FilterMode = "STUDIOS"
	FilterModeGalleries    FilterMode = "GALLERIES"
	FilterModeSceneMarkers FilterMode = "SCENE_MARKERS"
	FilterModeMovies       FilterMode = "MOVIES"
	FilterModeGroups       FilterMode = "GROUPS"
	FilterModeTags         FilterMode = "TAGS"
	FilterModeImages       FilterMode = "IMAGES"
)

var AllFilterMode = []FilterMode{
	FilterModeScenes,
	FilterModePerformers,
	FilterModeStudios,
	FilterModeGalleries,
	FilterModeSceneMarkers,
	FilterModeGroups,
	FilterModeMovies,
	FilterModeTags,
	FilterModeImages,
}

func (e FilterMode) IsValid() bool {
	switch e {
	case FilterModeScenes, FilterModePerformers, FilterModeStudios, FilterModeGalleries, FilterModeSceneMarkers, FilterModeMovies, FilterModeGroups, FilterModeTags, FilterModeImages:
		return true
	}
	return false
}

func (e FilterMode) String() string {
	return string(e)
}

func (e *FilterMode) UnmarshalGQL(v interface{}) error {
	str, ok := v.(string)
	if !ok {
		return fmt.Errorf("enums must be strings")
	}

	*e = FilterMode(str)
	if !e.IsValid() {
		return fmt.Errorf("%s is not a valid FilterMode", str)
	}
	return nil
}

func (e FilterMode) MarshalGQL(w io.Writer) {
	fmt.Fprint(w, strconv.Quote(e.String()))
}

type SavedFilter struct {
	ID         int             `db:"id" json:"id"`
	Mode       FilterMode      `db:"mode" json:"mode"`
	Name       string          `db:"name" json:"name"`
	FindFilter *FindFilterType `json:"find_filter"`
	// Legacy v2.5 criteria map. Persisted filters use FilterAST as the
	// canonical representation; this field only carries data for not-yet
	// migrated rows and legacy import files. The GraphQL object_filter
	// field is resolved by flattening FilterAST, not from this field.
	ObjectFilter map[string]interface{} `json:"object_filter"`
	FilterAST    *FilterAST             `json:"filter_ast"`
	UIOptions    map[string]interface{} `json:"ui_options"`
}
