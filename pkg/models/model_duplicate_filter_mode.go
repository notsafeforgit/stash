package models

import (
	"fmt"
	"io"
	"strconv"
)

type DuplicateFilterMode string

const (
	DuplicateFilterModeAll DuplicateFilterMode = "ALL"
	DuplicateFilterModeAny DuplicateFilterMode = "ANY"
)

var AllDuplicateFilterMode = []DuplicateFilterMode{
	DuplicateFilterModeAll,
	DuplicateFilterModeAny,
}

func (e DuplicateFilterMode) IsValid() bool {
	switch e {
	case DuplicateFilterModeAll, DuplicateFilterModeAny:
		return true
	}
	return false
}

func (e DuplicateFilterMode) String() string {
	return string(e)
}

func (e *DuplicateFilterMode) UnmarshalGQL(v interface{}) error {
	str, ok := v.(string)
	if !ok {
		return fmt.Errorf("enums must be strings")
	}

	*e = DuplicateFilterMode(str)
	if !e.IsValid() {
		return fmt.Errorf("%s is not a valid DuplicateFilterMode", str)
	}
	return nil
}

func (e DuplicateFilterMode) MarshalGQL(w io.Writer) {
	fmt.Fprint(w, strconv.Quote(e.String()))
}
