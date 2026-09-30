package performer

import (
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/assert"
)

func TestValidateName(t *testing.T) {
	assert.Equal(t, ErrNameMissing, ValidateName(""))
	assert.Equal(t, ErrNameMissing, ValidateName("  "))
	assert.NoError(t, ValidateName("A shared display name"))
	assert.NoError(t, ValidateName("A shared display name"))
}

func TestValidateUpdateName(t *testing.T) {
	assert.NoError(t, ValidateUpdateName(models.OptionalString{}))
	assert.Equal(t, ErrNameMissing, ValidateUpdateName(models.NewOptionalString("")))
	assert.NoError(t, ValidateUpdateName(models.NewOptionalString("Another performer's name")))
}

func TestValidateAliases(t *testing.T) {
	const (
		name1  = "name 1"
		name1U = "NAME 1"
		name2  = "name 2"
		name3  = "name 3"
		name4  = "name 4"
	)

	tests := []struct {
		tName   string
		name    string
		aliases []models.PerformerAlias
		want    error
	}{
		{"no aliases", name1, nil, nil},
		{"valid aliases", name2, []models.PerformerAlias{{Alias: name3}, {Alias: name4}}, nil},
		{"duplicate alias", name1, []models.PerformerAlias{{Alias: name2}, {Alias: name3}, {Alias: name2}}, &DuplicateAliasError{name2}},
		{"duplicate name", name4, []models.PerformerAlias{{Alias: name4}, {Alias: name3}}, &DuplicateAliasError{name4}},
		{"duplicate alias caps", name2, []models.PerformerAlias{{Alias: name1}, {Alias: name1U}}, &DuplicateAliasError{name1U}},
		{"duplicate name caps", name1U, []models.PerformerAlias{{Alias: name1}}, &DuplicateAliasError{name1}},
	}

	for _, tt := range tests {
		t.Run(tt.tName, func(t *testing.T) {
			got := ValidateAliases(tt.name, tt.aliases)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateUpdateAliases(t *testing.T) {
	const (
		name1  = "name 1"
		name1U = "NAME 1"
		name2  = "name 2"
		name3  = "name 3"
		name4  = "name 4"
	)

	existing := models.Performer{
		Name:    name1,
		Aliases: models.NewRelatedPerformerAliases([]models.PerformerAlias{{Alias: name2}}),
	}

	osUnset := models.OptionalString{}
	os1 := models.NewOptionalString(name1)
	os2 := models.NewOptionalString(name2)
	os3 := models.NewOptionalString(name3)
	os4 := models.NewOptionalString(name4)

	tests := []struct {
		tName   string
		name    models.OptionalString
		aliases []models.PerformerAlias
		want    error
	}{
		{"both unset", osUnset, nil, nil},
		{"promote existing alias", os2, nil, nil},
		{"valid name set", os3, nil, nil},
		{"valid aliases empty", os1, []models.PerformerAlias{}, nil},
		{"alias matches name", osUnset, []models.PerformerAlias{{Alias: name1U}}, &DuplicateAliasError{name1U}},
		{"valid aliases set", osUnset, []models.PerformerAlias{{Alias: name3}, {Alias: name2}}, nil},
		{"alias matches new name", os4, []models.PerformerAlias{{Alias: name4}}, &DuplicateAliasError{name4}},
		{"valid both set", os2, []models.PerformerAlias{{Alias: name1}}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.tName, func(t *testing.T) {
			var aliases *models.UpdatePerformerAliases
			if tt.aliases != nil {
				aliases = &models.UpdatePerformerAliases{
					Values: tt.aliases,
					Mode:   models.RelationshipUpdateModeSet,
				}
			}
			got := ValidateUpdateAliases(existing, tt.name, aliases)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateDeathDate(t *testing.T) {
	date1, _ := models.ParseDate("2001-01-01")
	date2, _ := models.ParseDate("2002-01-01")
	date3, _ := models.ParseDate("2003-01-01")
	date4, _ := models.ParseDate("2004-01-01")

	tests := []struct {
		name      string
		birthdate *models.Date
		deathdate *models.Date
		want      error
	}{
		{"both nil", nil, nil, nil},
		{"birthdate nil", nil, &date1, nil},
		{"deathdate nil", nil, &date2, nil},
		{"valid", &date3, &date4, nil},
		{"invalid", &date3, &date2, &DeathDateError{date3, date2}},
		{"same date", &date1, &date1, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateDeathDate(tt.birthdate, tt.deathdate)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateUpdateDeathDate(t *testing.T) {
	date1, _ := models.ParseDate("2001-01-01")
	date2, _ := models.ParseDate("2002-01-01")
	date3, _ := models.ParseDate("2003-01-01")
	date4, _ := models.ParseDate("2004-01-01")

	existing := models.Performer{
		Birthdate: &date2,
		DeathDate: &date3,
	}

	odUnset := models.OptionalDate{}
	odNull := models.OptionalDate{Set: true, Null: true}
	od1 := models.NewOptionalDate(date1)
	od2 := models.NewOptionalDate(date2)
	od3 := models.NewOptionalDate(date3)
	od4 := models.NewOptionalDate(date4)

	tests := []struct {
		name      string
		birthdate models.OptionalDate
		deathdate models.OptionalDate
		want      error
	}{
		{"both unset", odUnset, odUnset, nil},
		{"invalid birthdate set", od4, odUnset, &DeathDateError{date4, date3}},
		{"valid birthdate set", od1, odUnset, nil},
		{"valid birthdate set null", odNull, odUnset, nil},
		{"invalid deathdate set", odUnset, od1, &DeathDateError{date2, date1}},
		{"valid deathdate set", odUnset, od4, nil},
		{"valid deathdate set null", odUnset, odNull, nil},
		{"invalid both set", od3, od2, &DeathDateError{date3, date2}},
		{"valid both set", od2, od3, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateUpdateDeathDate(existing, tt.birthdate, tt.deathdate)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateCreate(t *testing.T) {
	tests := []struct {
		name    string
		pName   string
		aliases []models.PerformerAlias
	}{
		{"no aliases", "Performer 1", nil},
		{"empty aliases", "Performer 2", []models.PerformerAlias{}},
		{"alias matches name", "Performer 3", []models.PerformerAlias{{Alias: "Performer 3", IgnoreAutoTag: true}}},
		{"duplicate aliases", "Performer 4", []models.PerformerAlias{{Alias: "Alias 1", IgnoreAutoTag: true}, {Alias: "Alias 1", IgnoreAutoTag: false}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := models.Performer{
				Name: tt.pName,
			}
			p.Aliases = models.NewRelatedPerformerAliases(NormalizeAliases(p.Name, tt.aliases))

			// This should NOT panic
			err := ValidateCreate(p)
			assert.Nil(t, err)
		})
	}
}
