package archive

import (
	"sort"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

const MaxFileHistoryEditBytes = 4 << 20

// CatalogFileEdits retains unknown fields/representations as extensions. Null
// in the old catalog means inherit, never an explicit empty selected value.
func CatalogFileEdits(raw []byte) ([]models.SourceFileEdit, error) {
	object, err := DecodeJSONObject(raw, MaxFileHistoryEditBytes)
	if err != nil || len(object) == 0 || len(object) > 256 {
		return nil, models.ErrSourceFileHistoryInvalid
	}
	fields := make([]string, 0, len(object))
	for field := range object {
		if field == "" || len(field) > 128 || strings.ContainsRune(field, 0) {
			return nil, models.ErrSourceFileHistoryInvalid
		}
		fields = append(fields, field)
	}
	sort.Strings(fields)
	ret := make([]models.SourceFileEdit, 0, len(fields))
	for _, field := range fields {
		value := object[field]
		encoded, err := EncodeSourceJSON(value)
		if err != nil {
			return nil, err
		}
		edit := models.SourceFileEdit{Field: field, Mode: "set", Value: encoded}
		switch field {
		case "title", "details", "director":
			edit.TargetField, edit.ValueType = field, "string"
		case "date":
			edit.TargetField, edit.ValueType = field, "date"
		case "urls":
			edit.TargetField, edit.ValueType = field, "urls"
		case "actors":
			edit.TargetField, edit.ValueType = "performers", "names"
		case "tags", "studio":
			edit.TargetField, edit.ValueType = field, "names"
		case "movie":
			edit.TargetField, edit.ValueType = "groups", "names"
		}
		valid := edit.TargetField != ""
		switch {
		case value == nil:
			edit.Mode = "inherit"
		case field == "actors" || field == "tags" || field == "urls":
			values, ok := value.([]any)
			valid = valid && ok && len(values) > 0
			for _, v := range values {
				text, ok := v.(string)
				valid = valid && ok && strings.TrimSpace(text) != ""
			}
		default:
			text, ok := value.(string)
			valid = valid && ok && strings.TrimSpace(text) != ""
			if field == "date" {
				_, err := time.Parse("2006-01-02", text)
				valid = valid && err == nil
			}
		}
		if !valid {
			edit.TargetField, edit.ValueType, edit.Mode = "", "extension", "unmapped"
		}
		ret = append(ret, edit)
	}
	return ret, nil
}
