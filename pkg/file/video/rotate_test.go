package video

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stashapp/stash/pkg/ffmpeg"
)

type fakeRotationRemuxer struct {
	args ffmpeg.Args
}

func (f *fakeRotationRemuxer) Generate(_ context.Context, args ffmpeg.Args) error {
	f.args = slices.Clone(args)
	return os.WriteFile(args[len(args)-1], []byte("rotated"), 0o600)
}

type fakeRotationProber struct {
	rotation int64
	tag      string
	calls    int
}

func (f *fakeRotationProber) NewVideoFile(string) (*ffmpeg.VideoFile, error) {
	f.calls++
	stream := &ffmpeg.FFProbeStream{CodecType: "video"}
	if f.calls == 1 {
		stream.Tags.Rotate = "180"
	} else {
		stream.Tags.Rotate = f.tag
	}
	rotation := f.rotation
	if f.calls == 1 {
		rotation = 180
	}
	return &ffmpeg.VideoFile{
		Container:   "matroska,webm",
		Rotation:    rotation,
		VideoStream: stream,
	}, nil
}

func TestTargetRotation(t *testing.T) {
	tests := []struct {
		name      string
		current   int64
		direction RotationDirection
		want      int64
		wantClear bool
	}{
		{name: "clockwise", current: 180, direction: RotateCW, want: 90},
		{name: "clockwise wraps", current: 0, direction: RotateCW, want: 270},
		{name: "counter-clockwise", current: 180, direction: RotateCCW, want: 270},
		{name: "counter-clockwise wraps", current: 270, direction: RotateCCW, want: 0},
		{name: "normalizes current", current: -90, direction: RotateCCW, want: 0},
		{name: "clear", current: 180, direction: RotateClear, want: 0, wantClear: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, removeTag, err := targetRotation(test.current, test.direction)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want || removeTag != test.wantClear {
				t.Fatalf("targetRotation(%d, %d) = (%d, %t), want (%d, %t)", test.current, test.direction, got, removeTag, test.want, test.wantClear)
			}
		})
	}
}

func TestTargetRotationRejectsUnknownDirection(t *testing.T) {
	if _, _, err := targetRotation(0, RotationDirection(99)); err == nil {
		t.Fatal("targetRotation accepted an unknown direction")
	}
}

func TestIsMatroskaContainer(t *testing.T) {
	if !isMatroskaContainer("matroska,webm") {
		t.Fatal("combined Matroska/WebM format was not recognized")
	}
	if isMatroskaContainer("mov,mp4,m4a,3gp,3g2,mj2") {
		t.Fatal("MP4 was recognized as Matroska")
	}
}

func TestStageRotationMetadataAndRollback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "video.mkv")
	if err := os.WriteFile(path, []byte("original"), 0o640); err != nil {
		t.Fatal(err)
	}

	remuxer := &fakeRotationRemuxer{}
	prober := &fakeRotationProber{rotation: 90, tag: "90"}
	patch, err := StageRotationMetadata(
		context.Background(),
		remuxer,
		prober,
		path,
		RotateCW,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := patch.Rollback(); err != nil {
			t.Errorf("rolling back rotation patch: %v", err)
		}
	}()

	if patch.CurrentRotation != 180 || patch.Rotation != 90 {
		t.Fatalf("rotation patch = %d -> %d, want 180 -> 90", patch.CurrentRotation, patch.Rotation)
	}
	wantMetadata := []string{"-metadata:s:v:0", "ROTATE=90"}
	if !containsAdjacent(remuxer.args, wantMetadata) {
		t.Fatalf("ffmpeg args %q do not contain %q", remuxer.args, wantMetadata)
	}

	if err := patch.Apply(); err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, path, "rotated")
	if err := patch.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, path, "original")
}

func TestStageRotationMetadataClearAndCommit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "video.mkv")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	remuxer := &fakeRotationRemuxer{}
	prober := &fakeRotationProber{rotation: 0}
	patch, err := StageRotationMetadata(
		context.Background(),
		remuxer,
		prober,
		path,
		RotateClear,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := patch.Rollback(); err != nil {
			t.Errorf("rolling back rotation patch: %v", err)
		}
	}()

	wantMetadata := []string{"-metadata:s:v:0", "ROTATE="}
	if !containsAdjacent(remuxer.args, wantMetadata) {
		t.Fatalf("ffmpeg args %q do not contain %q", remuxer.args, wantMetadata)
	}
	if err := patch.Apply(); err != nil {
		t.Fatal(err)
	}
	if err := patch.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := patch.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, path, "rotated")
}

func containsAdjacent(values, want []string) bool {
	for i := 0; i+len(want) <= len(values); i++ {
		if slices.Equal(values[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s contains %q, want %q", path, got, want)
	}
}
