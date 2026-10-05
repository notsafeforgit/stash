package file

import (
	"archive/zip"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stashapp/stash/pkg/fsutil"
)

type deletionCapture struct {
	ctx        context.Context
	manifest   deletionSnapshotManifest
	roots      map[string]*os.Root
	paths      map[string]string
	nodes      map[string]int
	before     map[string]os.FileInfo
	full       map[string]bool
	identities map[string]int
	objects    int
	zip        *zip.Writer
	checkSpace func(int64) error
}

// CaptureDeletionSnapshot packages raw journals and their staged, replacement
// and reserved-trash trees into one regular ZIP component. It never recovers or
// mutates the source. The caller MUST hold the library's writer lock throughout
// capture and exclude external writers to these trees. Committed IDs must come
// from that same database checkpoint, including orphan markers.
//
// Parent directories are captured as skeletons, not as unrelated media trees.
// Unknown/corrupt journal entries, undeclared paths and observed changes fail
// capture. checkSpace is called during streaming to enforce the caller's reserve.
func CaptureDeletionSnapshot(ctx context.Context, journal, destination string, roots []DeletionSnapshotRoot, committed []string, checkSpace func(int64) error) (retErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	ids, err := deletionCommittedIDs(committed)
	if err != nil {
		return err
	}
	c := &deletionCapture{ctx: ctx, roots: make(map[string]*os.Root), paths: make(map[string]string),
		nodes: make(map[string]int), before: make(map[string]os.FileInfo), full: make(map[string]bool),
		identities: make(map[string]int), checkSpace: checkSpace,
		manifest: deletionSnapshotManifest{Format: deletionSnapshotFormat, Version: 1, Committed: ids}}
	defer func() {
		for _, root := range c.roots {
			retErr = errors.Join(retErr, root.Close())
		}
	}()
	if err := c.openRoots(roots); err != nil {
		return err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(destination))
	if err != nil {
		return err
	}
	destination = filepath.Join(parent, filepath.Base(destination))
	for _, root := range c.manifest.Roots {
		if deletionWithin(string(root.Path), destination) {
			return errors.New("deletion snapshot output must be outside its source roots")
		}
	}
	if checkSpace != nil {
		if err := checkSpace(0); err != nil {
			return err
		}
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, output.Close())
		if retErr != nil {
			_ = os.Remove(destination)
		}
	}()
	c.zip = zip.NewWriter(output)
	entries, validateJournal, closeJournal, err := captureDeletionJournals(journal)
	if err != nil {
		return err
	}
	defer closeJournal()
	c.manifest.Journals = entries
	var records []*deletionRecord
	for i := range c.manifest.Journals {
		entry := &c.manifest.Journals[i]
		r, err := decodeSnapshotJournal(*entry)
		if err != nil {
			return fmt.Errorf("capturing %q: %w", entry.Name, err)
		}
		if r == nil {
			continue
		}
		for _, path := range []string{r.Original, r.StageDir, r.Staged} {
			if _, err := deletionPathInRoots(c.manifest.Roots, path); err != nil {
				return err
			}
		}
		if r.TrashRoot != "" {
			resolved, err := canonicalDeletionDirectory(r.TrashRoot)
			if err != nil {
				return err
			}
			ref, err := deletionPathInRoots(c.manifest.Roots, resolved)
			if err != nil {
				return err
			}
			entry.TrashRoot = &ref
			if r.Destination != "" {
				rel, _ := filepath.Rel(r.TrashRoot, r.Destination)
				r.Destination = filepath.Join(resolved, rel)
			}
			r.TrashRoot = resolved
		}
		records = append(records, r)
	}
	for _, root := range c.manifest.Roots {
		if err := c.capture(string(root.Path), false); err != nil {
			return err
		}
	}
	for _, r := range records {
		stage, err := snapshotDeletionLocation(r.StageDir, records, make(map[string]bool))
		if err != nil {
			return err
		}
		parent, err := snapshotDeletionLocation(filepath.Dir(r.Original), records, make(map[string]bool))
		if err != nil {
			return err
		}
		for _, path := range []string{r.Original, stage} {
			if err := c.capture(path, true); err != nil {
				return err
			}
		}
		if err := c.capture(parent, false); err != nil {
			return err
		}
		if r.TrashRoot != "" {
			if err := c.capture(r.TrashRoot, false); err != nil {
				return err
			}
		}
		if r.Destination != "" {
			if err := c.capture(filepath.Dir(r.Destination), true); err != nil {
				return err
			}
		}
	}
	if err := c.revalidate(); err != nil {
		return err
	}
	if err := validateJournal(); err != nil {
		return err
	}
	// Do not publish a component the restore protocol cannot safely rebind.
	validation := &deletionRestore{manifest: c.manifest, objects: map[string]*zip.File{"manifest.json": {}},
		nodes: make(map[string]deletionSnapshotNode), roots: make(map[string]string)}
	for _, node := range c.manifest.Nodes {
		if node.Kind == "file" {
			validation.objects[node.Object] = &zip.File{FileHeader: zip.FileHeader{UncompressedSize64: uint64(node.Bytes)}}
		}
	}
	if err := validation.validate(c.manifest.Committed); err != nil {
		return err
	}
	body, err := json.Marshal(c.manifest)
	if err != nil {
		return err
	}
	if len(body) > deletionSnapshotManifestLimit {
		return errors.New("deletion snapshot manifest exceeds size limit")
	}
	if checkSpace != nil {
		// Include central-directory entries and the manifest, which are written
		// after the final media object. Small-file trees still consume space.
		if err := checkSpace(int64(len(body)) + int64(c.objects+1)*256 + 1024); err != nil {
			return err
		}
	}
	manifest, err := c.zip.CreateHeader(&zip.FileHeader{Name: "manifest.json", Method: zip.Store})
	if err != nil {
		return err
	}
	if _, err := manifest.Write(body); err != nil {
		return err
	}
	if err := errors.Join(c.zip.Close(), ctx.Err()); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	return fsutil.SyncDir(parent)
}

func (c *deletionCapture) openRoots(roots []DeletionSnapshotRoot) error {
	for _, spec := range roots {
		if !deletionRootName.MatchString(spec.Name) || c.roots[spec.Name] != nil {
			return errors.New("invalid or duplicate deletion snapshot root name")
		}
		path, err := filepath.Abs(spec.Path)
		if err != nil {
			return err
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		if filepath.Dir(path) == path {
			return errors.New("deletion snapshot requires explicit media roots, not the filesystem root")
		}
		for _, other := range c.manifest.Roots {
			if deletionWithin(string(other.Path), path) || deletionWithin(path, string(other.Path)) {
				return errors.New("deletion snapshot roots overlap")
			}
		}
		root, err := os.OpenRoot(path)
		if err != nil {
			return err
		}
		c.roots[spec.Name], c.paths[spec.Name] = root, path
		c.manifest.Roots = append(c.manifest.Roots, deletionSnapshotRoot{Name: spec.Name, Path: []byte(path)})
	}
	return nil
}

func (c *deletionCapture) capture(path string, tree bool) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	ref, err := deletionPathInRoots(c.manifest.Roots, path)
	if err != nil {
		return err
	}
	root, relative := c.roots[ref.Root], string(ref.Path)
	info, err := root.Lstat(relative)
	if errors.Is(err, os.ErrNotExist) {
		c.before[ref.key()] = nil
		return nil
	}
	if err != nil {
		return err
	}
	if relative != "." {
		if err := c.capture(filepath.Dir(path), false); err != nil {
			return err
		}
		parent := deletionSnapshotPath{Root: ref.Root, Path: []byte(filepath.Dir(relative))}
		index, ok := c.nodes[parent.key()]
		if !ok || c.manifest.Nodes[index].Kind != "directory" {
			return errors.New("deletion snapshot path traverses a symlink or unavailable parent")
		}
	}
	if _, found := c.nodes[ref.key()]; !found {
		identity, err := fsutil.FileIdentity(path)
		if err != nil {
			return err
		}
		node := deletionSnapshotNode{deletionSnapshotPath: ref, Identity: identity,
			Mode: uint32(info.Mode().Perm()), Modified: info.ModTime().UnixNano()}
		switch {
		case info.IsDir():
			node.Kind = "directory"
		case info.Mode()&os.ModeSymlink != 0:
			node.Kind = "symlink"
			target, err := root.Readlink(relative)
			if err != nil {
				return err
			}
			node.Target = []byte(target)
		case info.Mode().IsRegular():
			node.Kind, node.Bytes = "file", info.Size()
			if first, ok := c.identities[identity]; ok {
				previous := c.manifest.Nodes[first]
				if !sameDeletionSnapshotFile(c.before[previous.key()], info) {
					return errors.New("hard-linked file changed during deletion capture")
				}
				node.Object, node.SHA256 = previous.Object, previous.SHA256
			} else if err := c.copyFile(&node, root, info); err != nil {
				return err
			}
		default:
			return fmt.Errorf("deletion snapshot cannot capture special file %q", path)
		}
		c.nodes[ref.key()], c.identities[identity] = len(c.manifest.Nodes), len(c.manifest.Nodes)
		c.manifest.Nodes = append(c.manifest.Nodes, node)
		c.before[ref.key()] = info
	}
	if tree && info.IsDir() && !c.full[ref.key()] {
		c.full[ref.key()] = true
		directory, err := root.Open(relative)
		if err != nil {
			return err
		}
		entries, err := directory.ReadDir(-1)
		err = errors.Join(err, directory.Close())
		if err != nil {
			return err
		}
		slices.SortFunc(entries, func(a, b os.DirEntry) int { return cmp.Compare(a.Name(), b.Name()) })
		for _, entry := range entries {
			if err := c.capture(filepath.Join(path, entry.Name()), true); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *deletionCapture) copyFile(node *deletionSnapshotNode, root *os.Root, before os.FileInfo) error {
	input, err := fsutil.OpenRootRegularFile(root, string(node.Path))
	if err != nil {
		return err
	}
	defer input.Close()
	opened, err := input.Stat()
	if err != nil || !sameDeletionSnapshotFile(before, opened) {
		return errors.Join(err, errors.New("deletion source changed before copying"))
	}
	node.Object = deletionObjectName(c.objects)
	c.objects++
	object, err := c.zip.CreateHeader(&zip.FileHeader{Name: node.Object, Method: zip.Store})
	if err != nil {
		return err
	}
	digest := sha256.New()
	reader := &deletionSnapshotReader{ctx: c.ctx, reader: io.LimitReader(input, before.Size()), remaining: before.Size(), checkSpace: c.checkSpace}
	n, err := io.CopyBuffer(io.MultiWriter(object, digest), reader, make([]byte, 256<<10))
	if err != nil || n != before.Size() {
		return errors.Join(err, errors.New("deletion source copy was incomplete"))
	}
	after, err := input.Stat()
	if err != nil || !sameDeletionSnapshotFile(before, after) {
		return errors.Join(err, errors.New("deletion source changed while copying"))
	}
	node.SHA256 = hex.EncodeToString(digest.Sum(nil))
	return nil
}

func (c *deletionCapture) revalidate() error {
	for key, before := range c.before {
		if err := c.ctx.Err(); err != nil {
			return err
		}
		rootName, relative, _ := strings.Cut(key, "\x00")
		after, err := c.roots[rootName].Lstat(relative)
		if before == nil && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || before == nil || !sameDeletionSnapshotFile(before, after) {
			return fmt.Errorf("deletion path changed during snapshot: %q", relative)
		}
		// Also reject replacement of the root's path while its descriptor stayed
		// valid. The caller must restore the same observed filesystem boundary.
		current, err := os.Lstat(filepath.Join(c.paths[rootName], relative))
		if err != nil || !sameDeletionSnapshotFile(before, current) {
			return fmt.Errorf("deletion root/path changed during snapshot: %q", relative)
		}
	}
	return nil
}

func canonicalDeletionDirectory(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) || filepath.Dir(path) == path {
		return "", err
	}
	parent, err := canonicalDeletionDirectory(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

func captureDeletionJournals(path string) ([]deletionSnapshotJournal, func() error, func(), error) {
	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, func() error {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				return errors.New("deletion journal appeared during capture")
			}
			return nil
		}, func() {}, nil
	}
	if err != nil || !before.IsDir() {
		return nil, nil, nil, errors.Join(err, errors.New("deletion journal is not a regular directory"))
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, nil, nil, err
	}
	closeRoot := func() { _ = root.Close() }
	directory, err := root.Open(".")
	if err != nil {
		closeRoot()
		return nil, nil, nil, err
	}
	names, err := directory.Readdirnames(-1)
	err = errors.Join(err, directory.Close())
	if err != nil {
		closeRoot()
		return nil, nil, nil, err
	}
	slices.Sort(names)
	infos := make(map[string]os.FileInfo)
	var ret []deletionSnapshotJournal
	for _, name := range names {
		info, err := root.Lstat(name)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
			closeRoot()
			return nil, nil, nil, errors.Join(err, errors.New("deletion journal entry is not a bounded regular file"))
		}
		input, err := fsutil.OpenRootRegularFile(root, name)
		if err != nil {
			closeRoot()
			return nil, nil, nil, err
		}
		data, err := io.ReadAll(io.LimitReader(input, (1<<20)+1))
		err = errors.Join(err, input.Close())
		if err != nil || len(data) > 1<<20 {
			closeRoot()
			return nil, nil, nil, errors.Join(err, errors.New("deletion journal entry exceeds size limit"))
		}
		ret = append(ret, deletionSnapshotJournal{Name: name, Data: data})
		infos[name] = info
	}
	validate := func() error {
		after, err := os.Lstat(path)
		if err != nil || !sameDeletionSnapshotFile(before, after) {
			return errors.New("deletion journal directory changed during capture")
		}
		for name, info := range infos {
			after, err := root.Lstat(name)
			if err != nil || !sameDeletionSnapshotFile(info, after) {
				return errors.New("deletion journal entry changed during capture")
			}
		}
		return nil
	}
	return ret, validate, closeRoot, nil
}
