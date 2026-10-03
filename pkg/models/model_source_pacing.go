package models

import "errors"

var ErrSourcePacingAtomic = errors.New("source scheduling did not finish atomically")

type SourceRunServiceReservation struct {
	RunUUID string `json:"run_uuid"`
	Fence   int64  `json:"fence"`
	Scope   string `json:"source_scope"`
	Ready   bool   `json:"ready"`
}
