package archive

import (
	"context"

	"github.com/stashapp/stash/pkg/models"
)

// MediaFileInspection binds a local-file review to the file that was inspected.
// A platform change token avoids hashing large videos in a preview. Platforms
// without one use a digest so a same-size overwrite with restored mtime cannot
// silently change the reviewed file.
type MediaFileInspection struct {
	Snapshot FileSnapshot `json:"snapshot"`
	SHA256   string       `json:"sha256,omitempty"`
}

func InspectMediaFile(ctx context.Context, root models.MediaRoot, relative string) (*MediaFileInspection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	opened, err := OpenMediaRootFile(root, relative)
	if err != nil {
		return nil, err
	}
	snapshot, err := snapshotOpenFile(opened)
	closeErr := opened.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if snapshot.ChangeToken != "" {
		return &MediaFileInspection{Snapshot: snapshot}, nil
	}
	verified, err := VerifyMediaFile(ctx, root, relative, nil, "")
	if err != nil {
		return nil, err
	}
	defer verified.Close()
	return &MediaFileInspection{Snapshot: verified.Snapshot, SHA256: verified.SHA256}, nil
}

func (i MediaFileInspection) Matches(snapshot FileSnapshot, sha256 string) bool {
	return sameFileSnapshot(i.Snapshot, snapshot) && (i.SHA256 == "" || i.SHA256 == sha256)
}
