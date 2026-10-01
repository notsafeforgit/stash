package models

import (
	"context"
	"errors"
)

// FilePathFence is local publication state. Root UUID and relative path remain
// the producer-facing address; this snapshot guards deletion/rename even when
// no file row existed when work was accepted.
type FilePathFence struct {
	Path          string `json:"path"`
	CaseSensitive bool   `json:"case_sensitive"`
	Revision      int64  `json:"revision"`
}

var ErrFilePathChanged = errors.New("file path was removed or moved after intake was prepared")

type FilePathReader interface {
	Snapshot(context.Context, string, bool) (*FilePathFence, error)
	Check(context.Context, FilePathFence) error
}
