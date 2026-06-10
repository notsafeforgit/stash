package models

import "context"

type CustomFieldMap map[string]interface{}

type CustomFieldsInput struct {
	// If populated, the entire custom fields map will be replaced with this value
	Full map[string]interface{} `json:"full"`
	// If populated, only the keys in this map will be updated
	Partial map[string]interface{} `json:"partial"`
	// Remove any keys in this list
	Remove []string `json:"remove"`
}

type CustomFieldsReader interface {
	GetCustomFields(ctx context.Context, id int) (map[string]interface{}, error)
	GetCustomFieldsBulk(ctx context.Context, ids []int) ([]CustomFieldMap, error)
	// DistinctCustomFieldNames returns the set of custom field names that are
	// present on at least one row of this entity type, sorted alphabetically.
	// Used to populate the field-name picker in the filter UI.
	DistinctCustomFieldNames(ctx context.Context) ([]string, error)
}

type CustomFieldsWriter interface {
	SetCustomFields(ctx context.Context, id int, fields CustomFieldsInput) error
}
