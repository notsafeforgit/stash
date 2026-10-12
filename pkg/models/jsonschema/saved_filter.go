package jsonschema

import (
	"bytes"
	"encoding/json"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
)

type SavedFilter struct {
	Mode       models.FilterMode      `db:"mode" json:"mode"`
	Name       string                 `db:"name" json:"name"`
	FindFilter *models.FindFilterType `json:"find_filter"`
	FilterAST  *models.FilterAST      `json:"filter_ast,omitempty"`
	UIOptions  map[string]interface{} `json:"ui_options"`
}

func (s SavedFilter) Filename() string {
	ret := fsutil.SanitiseBasename(s.Name + "_" + s.Mode.String())
	return ret + ".json"
}

func LoadSavedFilterFile(filePath string) (*SavedFilter, error) {
	return loadFile[SavedFilter](filePath)
}

func SaveSavedFilterFile(filePath string, image *SavedFilter) error {
	return saveFile[SavedFilter](filePath, image)
}

// Reject retired formats instead of importing them as an empty filter.
func (s *SavedFilter) UnmarshalJSON(data []byte) error {
	type canonical SavedFilter
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value canonical
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	*s = SavedFilter(value)
	return nil
}
