package file

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
)

const deletionSnapshotFormat = "org.notsafeforgit.stash.deletion-snapshot"
const deletionSnapshotManifestLimit = 64 << 20

var deletionRootName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// DeletionSnapshotRoot explicitly authorizes a filesystem tree for capture.
// Names are portable identifiers; Path may contain arbitrary filesystem bytes.
// Roots must exist and must not overlap, including through directory aliases.
type DeletionSnapshotRoot struct {
	Name string
	Path string
}

type deletionSnapshotPath struct {
	Root string `json:"root"`
	Path []byte `json:"path"`
}

type deletionSnapshotRoot struct {
	Name string `json:"name"`
	Path []byte `json:"path"`
}

type deletionSnapshotJournal struct {
	Name      string                `json:"name"`
	Data      []byte                `json:"data"`
	TrashRoot *deletionSnapshotPath `json:"trash_root,omitempty"`
}

type deletionSnapshotNode struct {
	deletionSnapshotPath
	Identity string `json:"identity"`
	Kind     string `json:"kind"`
	Mode     uint32 `json:"mode"`
	Modified int64  `json:"modified"`
	Object   string `json:"object,omitempty"`
	Bytes    int64  `json:"bytes,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
	Target   []byte `json:"target,omitempty"`
}

type deletionSnapshotManifest struct {
	Format    string                    `json:"format"`
	Version   int                       `json:"version"`
	Committed []string                  `json:"committed"`
	Roots     []deletionSnapshotRoot    `json:"roots"`
	Journals  []deletionSnapshotJournal `json:"journals"`
	Nodes     []deletionSnapshotNode    `json:"nodes"`
}

func deletionObjectName(index int) string { return fmt.Sprintf("objects/%08d", index) }

func (p deletionSnapshotPath) key() string { return p.Root + "\x00" + string(p.Path) }

func deletionWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

func deletionPathInRoots(roots []deletionSnapshotRoot, path string) (deletionSnapshotPath, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return deletionSnapshotPath{}, fmt.Errorf("invalid deletion snapshot path %q", path)
	}
	for _, root := range roots {
		if deletionWithin(string(root.Path), path) {
			rel, _ := filepath.Rel(string(root.Path), path)
			return deletionSnapshotPath{Root: root.Name, Path: []byte(rel)}, nil
		}
	}
	return deletionSnapshotPath{}, fmt.Errorf("deletion path %q is outside the declared snapshot roots", path)
}

func deletionCommittedIDs(ids []string) ([]string, error) {
	ret := slices.Clone(ids)
	slices.Sort(ret)
	for i, id := range ret {
		if _, err := uuid.Parse(id); err != nil || (i != 0 && id == ret[i-1]) {
			return nil, errors.New("invalid or duplicate deletion commit marker")
		}
	}
	return ret, nil
}

func decodeSnapshotJournal(entry deletionSnapshotJournal) (*deletionRecord, error) {
	if len(entry.Data) > 1<<20 || filepath.Base(entry.Name) != entry.Name {
		return nil, errors.New("invalid deletion snapshot journal entry")
	}
	if strings.HasPrefix(entry.Name, ".prepare-") {
		if entry.TrashRoot != nil {
			return nil, errors.New("unfinished journal preparation has path bindings")
		}
		return nil, nil
	}
	var record deletionRecord
	decoder := gob.NewDecoder(bytes.NewReader(entry.Data))
	if err := decoder.Decode(&record); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(deletionRecord)); !errors.Is(err, io.EOF) {
		return nil, errors.New("deletion journal contains trailing data")
	}
	if err := record.validate(); err != nil {
		return nil, err
	}
	if entry.Name != record.ID+".journal" {
		return nil, errors.New("deletion snapshot journal ID differs from filename")
	}
	return &record, nil
}

// This resolver mirrors recovery but bounds recursion for damaged journals.
func snapshotDeletionLocation(path string, records []*deletionRecord, visiting map[string]bool) (string, error) {
	var closest *deletionRecord
	for _, r := range records {
		if r.Directory && deletionWithin(r.Original, path) && (closest == nil || len(r.Original) > len(closest.Original)) {
			closest = r
		}
	}
	if closest == nil || identityMatches(closest.Original, closest.SourceID) {
		return path, nil
	}
	if visiting[closest.ID] {
		return "", errors.New("cyclic deletion journal paths")
	}
	visiting[closest.ID] = true
	defer delete(visiting, closest.ID)
	base, err := snapshotDeletionLocation(closest.Staged, records, visiting)
	if err != nil {
		return "", err
	}
	if !identityMatches(base, closest.SourceID) {
		return path, nil
	}
	rel, _ := filepath.Rel(closest.Original, path)
	return filepath.Join(base, rel), nil
}

type deletionSnapshotReader struct {
	ctx        context.Context
	reader     io.Reader
	remaining  int64
	checkSpace func(int64) error
}

func (r *deletionSnapshotReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.checkSpace != nil {
		if err := r.checkSpace(max(0, r.remaining)); err != nil {
			return 0, err
		}
	}
	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	return n, err
}

func sameDeletionSnapshotFile(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.Mode() == b.Mode() &&
		a.ModTime().Equal(b.ModTime()) && deletionSnapshotChangeToken(a) == deletionSnapshotChangeToken(b)
}
