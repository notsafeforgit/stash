package performer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

var (
	ErrNameMissing = errors.New("performer name must not be blank")
)

type NotFoundError struct {
	id int
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("performer with id %d not found", e.id)
}

type DuplicateAliasError struct {
	Alias string
}

func (e *DuplicateAliasError) Error() string {
	return fmt.Sprintf("performer contains duplicate alias '%s'", e.Alias)
}

type DeathDateError struct {
	Birthdate models.Date
	DeathDate models.Date
}

func (e *DeathDateError) Error() string {
	return fmt.Sprintf("death date %s should be after birthdate %s", e.DeathDate, e.Birthdate)
}

func ValidateCreate(performer models.Performer) error {
	if err := ValidateName(performer.Name); err != nil {
		return err
	}

	if performer.Aliases.Loaded() {
		if err := ValidateAliases(performer.Name, performer.Aliases.List()); err != nil {
			return err
		}
	}

	if err := ValidateDeathDate(performer.Birthdate, performer.DeathDate); err != nil {
		return err
	}

	return nil
}

func ValidateUpdate(ctx context.Context, id int, partial models.PerformerPartial, qb models.PerformerReader) error {
	existing, err := qb.Find(ctx, id)
	if err != nil {
		return err
	}

	if existing == nil {
		return &NotFoundError{id}
	}

	if err := ValidateUpdateName(partial.Name); err != nil {
		return err
	}

	if err := existing.LoadAliases(ctx, qb); err != nil {
		return err
	}

	if err := ValidateUpdateAliases(*existing, partial.Name, partial.Aliases); err != nil {
		return err
	}

	if err := ValidateUpdateDeathDate(*existing, partial.Birthdate, partial.DeathDate); err != nil {
		return err
	}

	return nil
}

// ValidateName validates a display value, not identity. Different performers
// may have the same canonical name or alias, with or without disambiguation.
func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return ErrNameMissing
	}
	return nil
}

func ValidateUpdateName(name models.OptionalString) error {
	if !name.Set {
		return nil
	}
	return ValidateName(name.Value)
}

func ValidateAliases(name string, aliases []models.PerformerAlias) error {
	m := make(map[string]bool)
	nameL := strings.ToLower(name)
	m[nameL] = true

	for _, a := range aliases {
		aliasL := strings.ToLower(a.Alias)
		if m[aliasL] {
			return &DuplicateAliasError{a.Alias}
		}
		m[aliasL] = true
	}

	return nil
}

func ValidateUpdateAliases(existing models.Performer, name models.OptionalString, aliases *models.UpdatePerformerAliases) error {
	// if neither name nor aliases is set, don't check anything
	if !name.Set && aliases == nil {
		return nil
	}

	newName := existing.Name
	if name.Set {
		newName = name.Value
	}

	// Selecting an existing alias promotes it and retains the old canonical
	// name in its position. Other name changes keep the remaining aliases.
	if aliases == nil {
		names := append([]models.PerformerAlias(nil), existing.Aliases.List()...)
		for i := range names {
			if names[i].Alias == newName {
				names[i].Alias = existing.Name
			}
		}
		return ValidateAliases(newName, names)
	}

	newAliases := GetEffectiveAliases(existing.Aliases.List(), aliases.Values, aliases.Mode, false)

	return ValidateAliases(newName, newAliases)
}

// ValidateDeathDate returns an error if the birthdate is after the death date.
func ValidateDeathDate(birthdate *models.Date, deathDate *models.Date) error {
	if birthdate == nil || deathDate == nil {
		return nil
	}

	if birthdate.After(*deathDate) {
		return &DeathDateError{Birthdate: *birthdate, DeathDate: *deathDate}
	}

	return nil
}

// ValidateUpdateDeathDate performs the same check as ValidateDeathDate, but is used when modifying an existing performer.
func ValidateUpdateDeathDate(existing models.Performer, birthdate models.OptionalDate, deathDate models.OptionalDate) error {
	// if neither birthdate nor deathDate is set, don't check anything
	if !birthdate.Set && !deathDate.Set {
		return nil
	}

	newBirthdate := existing.Birthdate
	if birthdate.Set {
		newBirthdate = birthdate.Ptr()
	}

	newDeathDate := existing.DeathDate
	if deathDate.Set {
		newDeathDate = deathDate.Ptr()
	}

	return ValidateDeathDate(newBirthdate, newDeathDate)
}
