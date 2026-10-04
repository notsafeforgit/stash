package api

import (
	"fmt"
	"io"
	"strconv"
)

type BulkUpdateStatus string

const (
	BulkUpdateStatusCompleted BulkUpdateStatus = "COMPLETED"
	BulkUpdateStatusQueued    BulkUpdateStatus = "QUEUED"
)

func (s BulkUpdateStatus) MarshalGQL(w io.Writer) {
	fmt.Fprint(w, strconv.Quote(string(s)))
}

type BulkUpdateResult struct {
	Status        BulkUpdateStatus
	JobID         *string `gqlgen:"job_id"`
	SelectedCount int
	UpdatedIDs    []string `gqlgen:"updated_ids"`
}

func completedBulkUpdate(ids []int) *BulkUpdateResult {
	updated := make([]string, len(ids))
	for i, id := range ids {
		updated[i] = strconv.Itoa(id)
	}
	return &BulkUpdateResult{Status: BulkUpdateStatusCompleted, SelectedCount: len(ids), UpdatedIDs: updated}
}

func queuedBulkUpdate(jobID, selectedCount int) *BulkUpdateResult {
	id := strconv.Itoa(jobID)
	return &BulkUpdateResult{Status: BulkUpdateStatusQueued, JobID: &id, SelectedCount: selectedCount, UpdatedIDs: []string{}}
}
