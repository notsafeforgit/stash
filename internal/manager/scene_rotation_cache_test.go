package manager

import (
	"path/filepath"
	"testing"
)

func TestInvalidateVideoProbeCaches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "video.mkv")
	gopKey := path + "|100"
	rotationKey := path + "|200"
	otherKey := path + ".other|100"

	gopProbeCache.Store(gopKey, true)
	gopProbeCache.Store(otherKey, true)
	displayRotationProbeCache.Store(rotationKey, true)
	t.Cleanup(func() {
		gopProbeCache.Delete(gopKey)
		gopProbeCache.Delete(otherKey)
		displayRotationProbeCache.Delete(rotationKey)
	})

	InvalidateVideoProbeCaches(path)

	if _, ok := gopProbeCache.Load(gopKey); ok {
		t.Fatal("GOP probe cache entry was not removed")
	}
	if _, ok := displayRotationProbeCache.Load(rotationKey); ok {
		t.Fatal("display-rotation probe cache entry was not removed")
	}
	if _, ok := gopProbeCache.Load(otherKey); !ok {
		t.Fatal("unrelated GOP probe cache entry was removed")
	}
}
