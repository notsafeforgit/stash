package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
)

var ErrMediaFileChanged = errors.New("media file changed while being verified")
var ErrMediaFileDigest = errors.New("media file differs from its completion digest")

type FileSnapshot struct {
	Identity    string    `json:"identity"`
	Size        int64     `json:"size"`
	ModifiedAt  time.Time `json:"modified_at"`
	ChangeToken string    `json:"change_token"`
}

// VerifiedFile holds the descriptor used for hashing. Probing must use this
// descriptor as well. Revalidate immediately before committing any association.
type VerifiedFile struct {
	File     *os.File
	Snapshot FileSnapshot
	SHA256   string
	rootUUID string
	binding  models.MediaRootBinding
	relative string
}

func snapshotOpenFile(file *os.File) (FileSnapshot, error) {
	info, err := file.Stat()
	if err != nil {
		return FileSnapshot{}, err
	}
	if !info.Mode().IsRegular() {
		return FileSnapshot{}, errors.New("completion requires a regular file")
	}
	identity, err := fsutil.OpenFileIdentity(file)
	if err != nil {
		return FileSnapshot{}, err
	}
	return FileSnapshot{Identity: identity, Size: info.Size(), ModifiedAt: info.ModTime(), ChangeToken: fileChangeToken(info)}, nil
}

func sameFileSnapshot(a, b FileSnapshot) bool {
	return a.Identity == b.Identity && a.Size == b.Size && a.ModifiedAt.Equal(b.ModifiedAt) && a.ChangeToken == b.ChangeToken
}

type contextFileReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextFileReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func ValidSHA256(value string) bool {
	body, err := hex.DecodeString(value)
	return err == nil && len(body) == 32 && strings.ToLower(value) == value
}

// VerifyMediaFile hashes a bounded descriptor view and rejects observed changes.
// An optional producer digest and size validate what the completion event names;
// missing claims never become assertions that a producer verified the bytes.
func VerifyMediaFile(ctx context.Context, root models.MediaRoot, relative string, expectedSize *int64, expectedSHA256 string) (*VerifiedFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if (expectedSize != nil && *expectedSize < 0) || (expectedSHA256 != "" && !ValidSHA256(expectedSHA256)) {
		return nil, errors.New("invalid media completion size or SHA-256")
	}
	file, err := OpenMediaRootFile(root, relative)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			file.Close()
		}
	}()
	before, err := snapshotOpenFile(file)
	if err != nil {
		return nil, err
	}
	if expectedSize != nil && before.Size != *expectedSize {
		return nil, ErrMediaFileDigest
	}
	digest, n, err := hashOpenMediaFile(ctx, file, before.Size)
	if err != nil {
		return nil, err
	}
	after, err := snapshotOpenFile(file)
	if err != nil {
		return nil, err
	}
	if n != before.Size || !sameFileSnapshot(before, after) {
		return nil, ErrMediaFileChanged
	}
	if expectedSHA256 != "" && digest != expectedSHA256 {
		return nil, ErrMediaFileDigest
	}
	result := &VerifiedFile{File: file, Snapshot: after, SHA256: digest, rootUUID: root.UUID, binding: *root.Binding, relative: relative}
	if err := result.Revalidate(ctx, root); err != nil {
		return nil, err
	}
	keep = true
	return result, nil
}

func (v *VerifiedFile) Close() error { return v.File.Close() }

func hashOpenMediaFile(ctx context.Context, file *os.File, size int64) (string, int64, error) {
	hash := sha256.New()
	reader := contextFileReader{ctx: ctx, reader: io.NewSectionReader(file, 0, size)}
	n, err := io.CopyBuffer(hash, reader, make([]byte, 256<<10))
	return hex.EncodeToString(hash.Sum(nil)), n, err
}

func (v *VerifiedFile) Revalidate(ctx context.Context, root models.MediaRoot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if root.UUID != v.rootUUID || root.Binding == nil || *root.Binding != v.binding {
		return ErrMediaFileChanged
	}
	current, err := snapshotOpenFile(v.File)
	if err != nil {
		return err
	}
	if !sameFileSnapshot(v.Snapshot, current) {
		return ErrMediaFileChanged
	}
	// Reopening under the current reviewed root also detects replaced mounts,
	// renamed paths and symlink retargeting after hashing or probing.
	file, err := OpenMediaRootFile(root, v.relative)
	if err != nil {
		return err
	}
	defer file.Close()
	named, err := snapshotOpenFile(file)
	if err != nil {
		return err
	}
	if !sameFileSnapshot(v.Snapshot, named) {
		return ErrMediaFileChanged
	}
	// Without a platform change counter, size/mtime cannot detect a same-size
	// overwrite followed by restoring the old mtime. Rehash that descriptor.
	if v.Snapshot.ChangeToken == "" {
		digest, n, err := hashOpenMediaFile(ctx, file, named.Size)
		if err != nil {
			return err
		}
		if n != named.Size || digest != v.SHA256 {
			return ErrMediaFileChanged
		}
		final, err := snapshotOpenFile(file)
		if err != nil {
			return err
		}
		if !sameFileSnapshot(named, final) {
			return ErrMediaFileChanged
		}
		// Hashing may take time. Recheck the pathname/root afterwards, rather
		// than accepting a descriptor that was replaced during that read.
		latest, err := OpenMediaRootFile(root, v.relative)
		if err != nil {
			return err
		}
		defer latest.Close()
		final, err = snapshotOpenFile(latest)
		if err != nil {
			return err
		}
		if !sameFileSnapshot(named, final) {
			return ErrMediaFileChanged
		}
	}
	return nil
}
