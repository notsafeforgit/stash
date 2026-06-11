package sqlite

import (
	"gopkg.in/guregu/null.v4"

	"github.com/stashapp/stash/pkg/models"
)

// null package does not provide methods to convert null.Int to int pointer
func intFromPtr(i *int) null.Int {
	if i == nil {
		return null.NewInt(0, false)
	}

	return null.IntFrom(int64(*i))
}

func int64FromPtr(i *int64) null.Int {
	if i == nil {
		return null.NewInt(0, false)
	}

	return null.IntFrom(*i)
}

func nullIntPtr(i null.Int) *int {
	if !i.Valid {
		return nil
	}

	v := int(i.Int64)
	return &v
}

func nullInt64Ptr(i null.Int) *int64 {
	if !i.Valid {
		return nil
	}

	v := i.Int64
	return &v
}

func floatFromPtr(f *float64) null.Float {
	if f == nil {
		return null.NewFloat(0, false)
	}

	return null.FloatFrom(*f)
}

func stringFromPtr(s *string) null.String {
	if s == nil {
		return null.NewString("", false)
	}

	return null.StringFrom(*s)
}

func nullStringPtr(s null.String) *string {
	if !s.Valid {
		return nil
	}

	v := s.String
	return &v
}

func nullFloatPtr(i null.Float) *float64 {
	if !i.Valid {
		return nil
	}

	v := float64(i.Float64)
	return &v
}

func nullIntFolderIDPtr(i null.Int) *models.FolderID {
	if !i.Valid {
		return nil
	}

	v := models.FolderID(i.Int64)

	return &v
}

func nullIntFileIDPtr(i null.Int) *models.FileID {
	if !i.Valid {
		return nil
	}

	v := models.FileID(i.Int64)

	return &v
}

func nullIntFromFileIDPtr(i *models.FileID) null.Int {
	if i == nil {
		return null.NewInt(0, false)
	}

	return null.IntFrom(int64(*i))
}

func nullIntFromFolderIDPtr(i *models.FolderID) null.Int {
	if i == nil {
		return null.NewInt(0, false)
	}

	return null.IntFrom(int64(*i))
}
