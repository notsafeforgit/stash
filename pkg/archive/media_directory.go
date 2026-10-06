package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
)

var (
	ErrMediaDirectoryChanged = errors.New("media directory changed; restart the listing")
	ErrMediaDirectoryLimit   = errors.New("media directory contains too many entries for interactive listing")
	ErrMediaDirectoryInput   = errors.New("invalid media directory request")
)

const MaxMediaDirectoryEntries = 100000

type MediaDirectoryInput struct {
	Directory string
	After     string
	Signature string
	Query     string
	Limit     int
	// Scope binds pagination to the application's collection revision.
	Scope      string
	Extensions map[string]models.ArchiveEntityKind
}

type MediaDirectoryEntry struct {
	Name         string     `json:"name"`
	RelativePath string     `json:"relative_path"`
	Kind         string     `json:"kind"`
	Size         int64      `json:"size"`
	ModifiedAt   *time.Time `json:"modified_at,omitempty"`
}

type MediaDirectoryPage struct {
	Directory string                `json:"directory"`
	Signature string                `json:"signature"`
	Entries   []MediaDirectoryEntry `json:"entries"`
	NextAfter string                `json:"next_after,omitempty"`
}

func openMediaDirectory(root models.MediaRoot, directory string) (*os.Root, *os.File, error) {
	if root.State != "active" || root.Binding == nil || !ValidRootRelativePath(directory, true) {
		return nil, nil, ErrMediaDirectoryInput
	}
	pinned, _, err := openPinnedRoot(*root.Binding, true)
	if err != nil {
		return nil, nil, err
	}
	defer pinned.Close()
	child, err := pinned.OpenRoot(filepath.FromSlash(directory))
	if err != nil {
		return nil, nil, err
	}
	file, err := child.Open(".")
	if err != nil {
		child.Close()
		return nil, nil, err
	}
	return child, file, nil
}

func directorySnapshot(file *os.File) (FileSnapshot, error) {
	info, err := file.Stat()
	if err != nil {
		return FileSnapshot{}, err
	}
	if !info.IsDir() {
		return FileSnapshot{}, ErrMediaDirectoryInput
	}
	identity, err := fsutil.OpenFileIdentity(file)
	if err != nil {
		return FileSnapshot{}, err
	}
	return FileSnapshot{Identity: identity, Size: info.Size(), ModifiedAt: info.ModTime(), ChangeToken: fileChangeToken(info)}, nil
}

func directoryEntryKey(entry MediaDirectoryEntry) string {
	prefix := "1/"
	if entry.Kind == "directory" {
		prefix = "0/"
	}
	return prefix + entry.Name
}

// ReadMediaDirectory inspects one directory, never the library or a recursive
// tree. It retains only a page plus one candidate, and stats those candidates.
// Sorting a filesystem directory requires reading its names; the explicit cap
// and context checks bound that work and reject incomplete listings.
func ReadMediaDirectory(ctx context.Context, root models.MediaRoot, input MediaDirectoryInput) (*MediaDirectoryPage, error) {
	if input.Limit < 1 || input.Limit > 100 || len(input.Query) > 256 ||
		(input.After == "") != (input.Signature == "") || (input.Signature != "" && !ValidSHA256(input.Signature)) {
		return nil, ErrMediaDirectoryInput
	}
	if input.After != "" && (!strings.HasPrefix(input.After, "0/") && !strings.HasPrefix(input.After, "1/") ||
		!ValidRootRelativePath(input.After[2:], false) || strings.Contains(input.After[2:], "/")) {
		return nil, ErrMediaDirectoryInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pinned, directory, err := openMediaDirectory(root, input.Directory)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	defer pinned.Close()
	before, err := directorySnapshot(directory)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(struct {
		RootUUID   string
		Revision   int
		Directory  string
		Scope      string
		Query      string
		Extensions map[string]models.ArchiveEntityKind
		Snapshot   FileSnapshot
	}{root.UUID, root.Revision, input.Directory, input.Scope, input.Query, input.Extensions, before})
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(body)
	signature := hex.EncodeToString(digest[:])
	if input.Signature != "" && signature != input.Signature {
		return nil, ErrMediaDirectoryChanged
	}
	page := &MediaDirectoryPage{Directory: input.Directory, Signature: signature, Entries: []MediaDirectoryEntry{}}
	query, visited := strings.ToLower(input.Query), 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entries, readErr := directory.ReadDir(256)
		visited += len(entries)
		if visited > MaxMediaDirectoryEntries {
			return nil, ErrMediaDirectoryLimit
		}
		for _, entry := range entries {
			name := entry.Name()
			if !ValidRootRelativePath(name, false) || strings.Contains(name, "/") || entry.Type()&os.ModeSymlink != 0 ||
				strings.HasSuffix(strings.ToLower(name), ".part") || !strings.Contains(strings.ToLower(name), query) {
				continue
			}
			candidate := MediaDirectoryEntry{Name: name, RelativePath: path.Join(input.Directory, name)}
			switch {
			case entry.IsDir():
				candidate.Kind = "directory"
			case entry.Type().IsRegular():
				candidate.Kind = string(input.Extensions[strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")])
				if candidate.Kind != "scene" && candidate.Kind != "image" {
					continue
				}
			default:
				continue
			}
			key := directoryEntryKey(candidate)
			if key <= input.After || (len(page.Entries) == input.Limit+1 && key >= directoryEntryKey(page.Entries[len(page.Entries)-1])) {
				continue
			}

			page.Entries = append(page.Entries, candidate)
			sort.Slice(page.Entries, func(i, j int) bool { return directoryEntryKey(page.Entries[i]) < directoryEntryKey(page.Entries[j]) })
			if len(page.Entries) > input.Limit+1 {
				page.Entries = page.Entries[:input.Limit+1]
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	for i := range page.Entries {
		candidate := &page.Entries[i]
		info, err := pinned.Lstat(candidate.Name)
		if err != nil {
			return nil, ErrMediaDirectoryChanged
		}
		if candidate.Kind == "directory" {
			if !info.IsDir() {
				return nil, ErrMediaDirectoryChanged
			}
		} else {
			if !info.Mode().IsRegular() {
				return nil, ErrMediaDirectoryChanged
			}
			candidate.Size = info.Size()
			modified := info.ModTime().UTC()
			candidate.ModifiedAt = &modified
		}
	}
	currentRoot, current, err := openMediaDirectory(root, input.Directory)
	if err != nil {
		return nil, ErrMediaDirectoryChanged
	}
	defer currentRoot.Close()
	defer current.Close()
	after, err := directorySnapshot(current)
	if err != nil || !sameFileSnapshot(before, after) {
		return nil, ErrMediaDirectoryChanged
	}
	if len(page.Entries) > input.Limit {
		page.Entries = page.Entries[:input.Limit]
		page.NextAfter = directoryEntryKey(page.Entries[len(page.Entries)-1])
	}
	return page, nil
}
