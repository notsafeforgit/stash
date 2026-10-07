package gallery

import "github.com/stashapp/stash/pkg/models"

// DescribeAlbumJob reads the original plan and any committed publication without
// changing current source choices, admitting work or retrying notifications.
func DescribeAlbumJob(current *models.ArchiveJob) (*AlbumBackfillStatus, error) {
	return albumStatus(current)
}
