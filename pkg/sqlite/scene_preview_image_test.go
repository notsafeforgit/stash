//go:build integration

package sqlite_test

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/hash/md5"
)

func TestScenePreviewCoverIdentity(t *testing.T) {
	if err := withTxn(func(ctx context.Context) error {
		id := sceneIDs[sceneIdxWithGallery]
		// The ordinary v2.5 cover writer owns this identity. V3 must observe
		// replacements and clears without touching any sidecar state.
		for _, data := range [][]byte{[]byte("generated cover"), []byte("uploaded cover"), nil} {
			if err := db.Scene.UpdateCover(ctx, id, data); err != nil {
				return err
			}
			scene, err := db.Scene.Find(ctx, id)
			if err != nil {
				return err
			}
			want := ""
			if len(data) > 0 {
				want = md5.FromBytes(data)
			}
			if scene.CoverChecksum != want {
				t.Fatalf("cover identity = %s, want %s", scene.CoverChecksum, want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
