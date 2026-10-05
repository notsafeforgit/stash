package file

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/fsutil"
)

type RestoredDeletionRoot struct {
	Name         string
	OriginalPath string
	Path         string
}

// RestoredDeletionSnapshot describes isolated, rebound pending work. It does not
// certify the rest of the media/library or mean recovery has been performed.
// UnresolvedIdentities remain deliberately unmatchable in the rebound journals:
// absence/replacement must not turn into permission to delete a different file.
type RestoredDeletionSnapshot struct {
	JournalPath          string
	Roots                []RestoredDeletionRoot
	UnresolvedIdentities []string
}

type deletionRestore struct {
	manifest deletionSnapshotManifest
	objects  map[string]*zip.File
	nodes    map[string]deletionSnapshotNode
	roots    map[string]string
}

// RestoreDeletionSnapshot verifies a captured component and reconstructs it in
// a NEW private directory. It never writes to the original absolute paths or
// invokes recovery. expectedCommitted must be read from the matching copied
// database before normal Database.Open (which performs deletion recovery).
//
// JournalPath can be moved to that database's new journal path once its media
// root bindings and remaining archive components have also been restored. The
// caller must not activate the library on the strength of this component alone.
func RestoreDeletionSnapshot(ctx context.Context, source, destination string, expectedCommitted []string, checkSpace func(int64) error) (_ *RestoredDeletionSnapshot, retErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	before, err := os.Lstat(source)
	if err != nil || !before.Mode().IsRegular() {
		return nil, errors.Join(err, errors.New("deletion snapshot must be a regular file"))
	}
	input, err := os.Open(source)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	opened, err := input.Stat()
	if err != nil || !sameDeletionSnapshotFile(before, opened) {
		return nil, errors.Join(err, errors.New("deletion snapshot changed before opening"))
	}
	archive, err := zip.NewReader(input, before.Size())
	if err != nil {
		return nil, err
	}
	r, err := readDeletionSnapshot(archive, expectedCommitted)
	if err != nil {
		return nil, err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return nil, err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(destination))
	if err != nil {
		return nil, err
	}
	destination = filepath.Join(parent, filepath.Base(destination))
	for _, root := range r.manifest.Roots {
		if deletionWithin(string(root.Path), destination) {
			return nil, errors.New("deletion restore must be outside the original source roots")
		}
	}
	if checkSpace != nil {
		if err := checkSpace(0); err != nil {
			return nil, err
		}
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return nil, err
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, os.RemoveAll(destination))
		}
	}()
	root, err := os.OpenRoot(destination)
	if err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, root.Close()) }()
	if err := root.Mkdir("roots", 0700); err != nil {
		return nil, err
	}
	result := &RestoredDeletionSnapshot{JournalPath: filepath.Join(destination, "journal")}
	for _, spec := range r.manifest.Roots {
		result.Roots = append(result.Roots, RestoredDeletionRoot{Name: spec.Name,
			OriginalPath: string(spec.Path), Path: filepath.Join(destination, "roots", spec.Name)})
	}
	identities, err := r.restoreNodes(ctx, root, destination, checkSpace)
	if err != nil {
		return nil, err
	}
	missing := make(map[string]bool)
	bindIdentity := func(id string) string {
		if id == "" {
			return ""
		}
		if replacement := identities[id]; replacement != "" {
			return replacement
		}
		missing[id] = true
		// FileIdentity returns only hexadecimal components separated by colons.
		// This sentinel can never accidentally equal a reused inode on restore.
		return "unavailable-in-restored-snapshot:" + id
	}
	for _, entry := range r.manifest.Journals {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if checkSpace != nil {
			if err := checkSpace(int64(len(entry.Data)) + 4096); err != nil {
				return nil, err
			}
		}
		record, err := decodeSnapshotJournal(entry)
		if err != nil {
			return nil, err
		}
		if record == nil {
			if err := os.MkdirAll(result.JournalPath, 0700); err != nil {
				return nil, err
			}
			if err := writeDeletionSnapshotFile(filepath.Join(result.JournalPath, entry.Name), entry.Data); err != nil {
				return nil, err
			}
			continue
		}
		if err := r.rebindPaths(record, entry, destination); err != nil {
			return nil, err
		}
		record.SourceID, record.ParentID, record.StageDirID = bindIdentity(record.SourceID), bindIdentity(record.ParentID), bindIdentity(record.StageDirID)
		record.TrashRootID, record.TrashDirID = bindIdentity(record.TrashRootID), bindIdentity(record.TrashDirID)
		record.CopyDirID, record.CopyID = bindIdentity(record.CopyDirID), bindIdentity(record.CopyID)
		if err := writeDeletionRecord(result.JournalPath, record); err != nil {
			return nil, err
		}
	}
	for id := range missing {
		result.UnresolvedIdentities = append(result.UnresolvedIdentities, id)
	}
	slices.Sort(result.UnresolvedIdentities)
	after, err := os.Lstat(source)
	if err != nil || !sameDeletionSnapshotFile(before, after) {
		return nil, errors.New("deletion snapshot changed during restore")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := r.finishDirectories(ctx, root); err != nil {
		return nil, err
	}
	if err := errors.Join(fsutil.SyncDir(destination), fsutil.SyncDir(parent)); err != nil {
		return nil, err
	}
	return result, nil
}

func readDeletionSnapshot(archive *zip.Reader, expectedCommitted []string) (*deletionRestore, error) {
	if len(archive.File) == 0 {
		return nil, errors.New("deletion snapshot has no manifest")
	}
	r := &deletionRestore{objects: make(map[string]*zip.File), nodes: make(map[string]deletionSnapshotNode), roots: make(map[string]string)}
	for i, entry := range archive.File {
		want := deletionObjectName(i)
		if i == len(archive.File)-1 {
			want = "manifest.json"
		}
		if entry.Name != want || entry.Method != zip.Store || entry.Flags&1 != 0 || entry.CompressedSize64 != entry.UncompressedSize64 {
			return nil, errors.New("invalid deletion snapshot object inventory")
		}
		r.objects[entry.Name] = entry
	}
	manifest := archive.File[len(archive.File)-1]
	if manifest.UncompressedSize64 > deletionSnapshotManifestLimit {
		return nil, errors.New("deletion snapshot manifest exceeds size limit")
	}
	reader, err := manifest.Open()
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(reader, deletionSnapshotManifestLimit+1))
	err = errors.Join(err, reader.Close())
	if err != nil || len(body) > deletionSnapshotManifestLimit {
		return nil, errors.Join(err, errors.New("invalid deletion snapshot manifest"))
	}
	if err := json.Unmarshal(body, &r.manifest); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(r.manifest)
	if err != nil || !bytes.Equal(body, canonical) {
		return nil, errors.New("deletion snapshot manifest is not canonical or contains unknown/duplicate fields")
	}
	if err := r.validate(expectedCommitted); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *deletionRestore) validate(expectedCommitted []string) error {
	if r.manifest.Format != deletionSnapshotFormat || r.manifest.Version != 1 {
		return errors.New("unsupported deletion snapshot format/version")
	}
	expected, err := deletionCommittedIDs(expectedCommitted)
	if err != nil {
		return err
	}
	actual, err := deletionCommittedIDs(r.manifest.Committed)
	if err != nil || !slices.Equal(actual, r.manifest.Committed) || !slices.Equal(actual, expected) {
		return errors.New("deletion snapshot commit markers differ from the database checkpoint")
	}
	for _, root := range r.manifest.Roots {
		path := string(root.Path)
		if !deletionRootName.MatchString(root.Name) || r.roots[root.Name] != "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == path || strings.ContainsRune(path, 0) {
			return errors.New("invalid deletion snapshot root")
		}
		for _, other := range r.roots {
			if deletionWithin(path, other) || deletionWithin(other, path) {
				return errors.New("overlapping deletion snapshot roots")
			}
		}
		r.roots[root.Name] = path
	}
	identities := make(map[string]deletionSnapshotNode)
	objects := make(map[string]string)
	for _, node := range r.manifest.Nodes {
		if err := r.validateNode(node); err != nil {
			return err
		}
		if _, found := r.nodes[node.key()]; found {
			return errors.New("duplicate deletion snapshot path")
		}
		if previous, found := identities[node.Identity]; found {
			if node.Kind == "directory" || previous.Kind != node.Kind || previous.Mode != node.Mode || previous.Modified != node.Modified ||
				previous.Object != node.Object || previous.Bytes != node.Bytes || previous.SHA256 != node.SHA256 || !bytes.Equal(previous.Target, node.Target) {
				return errors.New("inconsistent deletion snapshot hard-link identity")
			}
		}
		identities[node.Identity] = node
		if node.Kind == "file" {
			if owner := objects[node.Object]; owner != "" && owner != node.Identity {
				return errors.New("deletion snapshot object aliases distinct file identities")
			}
			objects[node.Object] = node.Identity
		}
		r.nodes[node.key()] = node
	}
	if len(objects) != len(r.objects)-1 {
		return errors.New("deletion snapshot contains unreferenced objects")
	}
	for name := range r.roots {
		if r.nodes[(deletionSnapshotPath{Root: name, Path: []byte(".")}).key()].Kind != "directory" {
			return errors.New("deletion snapshot is missing a root directory")
		}
	}
	for _, node := range r.manifest.Nodes {
		if string(node.Path) != "." {
			parent := deletionSnapshotPath{Root: node.Root, Path: []byte(filepath.Dir(string(node.Path)))}
			if r.nodes[parent.key()].Kind != "directory" {
				return errors.New("deletion snapshot node has a missing or non-directory parent")
			}
		}
	}
	journals := make(map[string]bool)
	for _, entry := range r.manifest.Journals {
		if journals[entry.Name] {
			return errors.New("duplicate deletion snapshot journal")
		}
		journals[entry.Name] = true
		record, err := decodeSnapshotJournal(entry)
		if err != nil {
			return err
		}
		if record != nil {
			// Preflight every future journal path before creating any output.
			if err := r.rebindPaths(record, entry, filepath.Join(os.TempDir(), "stash-restore-placeholder")); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *deletionRestore) validateRef(ref deletionSnapshotPath) error {
	path := string(ref.Path)
	if r.roots[ref.Root] == "" || !filepath.IsLocal(path) || filepath.Clean(path) != path || strings.ContainsRune(path, 0) {
		return errors.New("invalid deletion snapshot relative path")
	}
	return nil
}

func (r *deletionRestore) validateNode(node deletionSnapshotNode) error {
	if err := r.validateRef(node.deletionSnapshotPath); err != nil {
		return err
	}
	if node.Identity == "" || node.Mode > 0777 {
		return errors.New("invalid deletion snapshot identity or permissions")
	}
	switch node.Kind {
	case "directory", "symlink":
		if node.Object != "" || node.Bytes != 0 || node.SHA256 != "" || (node.Kind == "directory" && len(node.Target) != 0) ||
			(node.Kind == "symlink" && (len(node.Target) == 0 || bytes.ContainsRune(node.Target, 0))) {
			return errors.New("invalid deletion snapshot directory/symlink metadata")
		}
	case "file":
		object := r.objects[node.Object]
		digest, err := hex.DecodeString(node.SHA256)
		if node.Bytes < 0 || object == nil || node.Object == "manifest.json" || object.UncompressedSize64 != uint64(node.Bytes) ||
			err != nil || len(digest) != sha256.Size || strings.ToLower(node.SHA256) != node.SHA256 || len(node.Target) != 0 {
			return errors.New("invalid deletion snapshot file metadata")
		}
	default:
		return errors.New("unsupported deletion snapshot node kind")
	}
	return nil
}

func (r *deletionRestore) rebindPaths(record *deletionRecord, entry deletionSnapshotJournal, destination string) error {
	mapRef := func(ref deletionSnapshotPath, directory bool) (string, error) {
		if err := r.validateRef(ref); err != nil {
			return "", err
		}
		path := string(ref.Path)
		ancestor := path
		if !directory {
			ancestor = filepath.Dir(path)
		}
		for {
			node, exists := r.nodes[(deletionSnapshotPath{Root: ref.Root, Path: []byte(ancestor)}).key()]
			if exists && node.Kind != "directory" {
				return "", errors.New("restored deletion journal would traverse a symlink or non-directory")
			}
			if ancestor == "." {
				break
			}
			ancestor = filepath.Dir(ancestor)
		}
		return filepath.Join(destination, "roots", ref.Root, path), nil
	}
	mapPath := func(path string, directory bool) (string, error) {
		ref, err := deletionPathInRoots(r.manifest.Roots, path)
		if err != nil {
			return "", err
		}
		return mapRef(ref, directory)
	}
	var err error
	record.Original, err = mapPath(record.Original, false)
	if err != nil {
		return err
	}
	record.StageDir, err = mapPath(record.StageDir, true)
	if err != nil {
		return err
	}
	record.Staged = filepath.Join(record.StageDir, filepath.Base(record.Original))
	if record.TrashRoot == "" {
		if entry.TrashRoot != nil {
			return errors.New("deletion snapshot has an unexpected trash root binding")
		}
	} else {
		if entry.TrashRoot == nil {
			return errors.New("deletion snapshot is missing its trash root binding")
		}
		trash, err := mapRef(*entry.TrashRoot, true)
		if err != nil {
			return err
		}
		if record.Destination != "" {
			rel, _ := filepath.Rel(record.TrashRoot, record.Destination)
			destRef := deletionSnapshotPath{Root: entry.TrashRoot.Root, Path: []byte(filepath.Join(string(entry.TrashRoot.Path), rel))}
			record.Destination, err = mapRef(destRef, false)
			if err != nil {
				return err
			}
		}
		record.TrashRoot = trash
	}
	return record.validate()
}

func deletionRestoredNodePath(node deletionSnapshotNode) string {
	return filepath.Join("roots", node.Root, string(node.Path))
}

func (r *deletionRestore) restoreNodes(ctx context.Context, root *os.Root, destination string, checkSpace func(int64) error) (map[string]string, error) {
	nodes := slices.Clone(r.manifest.Nodes)
	slices.SortStableFunc(nodes, func(a, b deletionSnapshotNode) int { return len(a.Path) - len(b.Path) })
	for _, node := range nodes {
		if node.Kind == "directory" {
			if err := root.Mkdir(deletionRestoredNodePath(node), 0700); err != nil {
				return nil, err
			}
		}
	}
	links := make(map[string]string)
	identities := make(map[string]string)
	for _, node := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := deletionRestoredNodePath(node)
		if existing := links[node.Identity]; existing != "" {
			if err := root.Link(existing, path); err != nil {
				return nil, err
			}
		} else {
			switch node.Kind {
			case "file":
				if err := restoreDeletionObject(ctx, root, path, r.objects[node.Object], node, checkSpace); err != nil {
					return nil, err
				}
			case "symlink":
				if err := root.Symlink(string(node.Target), path); err != nil {
					return nil, err
				}
			}
		}
		links[node.Identity] = path
		identity, err := fsutil.FileIdentity(filepath.Join(destination, path))
		if err != nil {
			return nil, err
		}
		identities[node.Identity] = identity
	}
	return identities, nil
}

func restoreDeletionObject(ctx context.Context, root *os.Root, path string, object *zip.File, node deletionSnapshotNode, checkSpace func(int64) error) (retErr error) {
	input, err := object.Open()
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := root.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, output.Close()) }()
	digest := sha256.New()
	reader := &deletionSnapshotReader{ctx: ctx, reader: input, remaining: node.Bytes, checkSpace: checkSpace}
	n, err := io.CopyBuffer(io.MultiWriter(output, digest), reader, make([]byte, 256<<10))
	if err != nil || n != node.Bytes || hex.EncodeToString(digest.Sum(nil)) != node.SHA256 {
		return errors.Join(err, errors.New("deletion snapshot file size or digest mismatch"))
	}
	if err := output.Chmod(os.FileMode(node.Mode)); err != nil {
		return err
	}
	when := time.Unix(0, node.Modified)
	if err := root.Chtimes(path, when, when); err != nil {
		return err
	}
	return output.Sync()
}

func (r *deletionRestore) finishDirectories(ctx context.Context, root *os.Root) error {
	nodes := slices.Clone(r.manifest.Nodes)
	slices.SortStableFunc(nodes, func(a, b deletionSnapshotNode) int { return len(b.Path) - len(a.Path) })
	for _, node := range nodes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if node.Kind != "directory" {
			continue
		}
		path := deletionRestoredNodePath(node)
		directory, err := root.Open(path)
		if err != nil {
			return err
		}
		when := time.Unix(0, node.Modified)
		err = errors.Join(directory.Chmod(os.FileMode(node.Mode)), root.Chtimes(path, when, when), directory.Sync(), directory.Close())
		if err != nil {
			return err
		}
	}
	directory, err := root.Open("roots")
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func writeDeletionSnapshotFile(path string, data []byte) (retErr error) {
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, output.Close()) }()
	if _, err := output.Write(data); err != nil {
		return err
	}
	return errors.Join(output.Sync(), fsutil.SyncDir(filepath.Dir(path)))
}
