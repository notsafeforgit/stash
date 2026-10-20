package covermigration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

// SourceKey freezes the old recipe's exact path/size/nanosecond/checksum
// semantics. The caller uses the same stat snapshot for the native fingerprint.
func SourceKey(path string, size, modTimeNano int64, checksum string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("1\x00%s\x00%d\x00%d\x00%s", path, size, modTimeNano, checksum))))
}

// Claims ignores malformed cache entries, just as the old reader did, but
// surfaces I/O errors so an unavailable cache cannot be checkpointed as empty.
// Rendition files and current encoder recipes have no bearing on the selection.
func Claims(ctx context.Context, root string, sceneID int) ([]models.SceneCoverClaim, error) {
	parent := filepath.Join(root, "1", strconv.Itoa(sceneID), "cover")
	dir, err := os.Open(parent)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	var ret []models.SceneCoverClaim
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entries, err := dir.ReadDir(64)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		for _, entry := range entries {
			key := entry.Name()
			if !entry.IsDir() || len(key) != 64 || strings.ToLower(key) != key {
				continue
			}
			if _, err := hex.DecodeString(key); err != nil {
				continue
			}
			claim, err := readClaim(filepath.Join(parent, key, "manifest.json"), key)
			if err != nil {
				return nil, err
			}
			if claim != nil {
				ret = append(ret, *claim)
				if len(ret) > 256 {
					return nil, errors.New("more than 256 historical cover selections for one scene; preserve and review the cache")
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return ret, nil
		}
	}
}

func readClaim(path, key string) (*models.SceneCoverClaim, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("cover manifest is not a regular file: %s", path)
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("cover manifest is not a regular file: %s", path)
	}
	const maxSize = 64 << 10
	data, err := io.ReadAll(io.LimitReader(f, maxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSize {
		return nil, fmt.Errorf("historical cover manifest exceeds 64 KiB: %s", path)
	}
	var saved struct {
		Version int      `json:"version"`
		Key     string   `json:"key"`
		At      *float64 `json:"at"`
	}
	if json.Unmarshal(data, &saved) != nil || saved.Version != 1 || saved.Key != key ||
		saved.At == nil || *saved.At < 0 || math.IsNaN(*saved.At) || math.IsInf(*saved.At, 0) {
		return nil, nil
	}
	return &models.SceneCoverClaim{Key: key, At: *saved.At}, nil
}
