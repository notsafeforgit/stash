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

type fakeRotationProbeResult struct {
	container string
	rotation  int64
	tag       string
}

type fakeRotationProber struct {
	before fakeRotationProbeResult
	after  fakeRotationProbeResult
	calls  int
}

func (f *fakeRotationProber) NewVideoFile(string) (*ffmpeg.VideoFile, error) {
	f.calls++
	result := f.after
	if f.calls == 1 {
		result = f.before
	}
	stream := &ffmpeg.FFProbeStream{CodecType: "video"}
	stream.Tags.Rotate = result.tag
	return &ffmpeg.VideoFile{
		Container:   result.container,
		Rotation:    result.rotation,
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
			got, clearMetadata, err := targetRotation(test.current, test.direction)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want || clearMetadata != test.wantClear {
				t.Fatalf("targetRotation(%d, %d) = (%d, %t), want (%d, %t)", test.current, test.direction, got, clearMetadata, test.want, test.wantClear)
			}
		})
	}
}

func TestTargetRotationRejectsUnknownDirection(t *testing.T) {
	if _, _, err := targetRotation(0, RotationDirection(99)); err == nil {
		t.Fatal("targetRotation accepted an unknown direction")
	}
}

func TestSignedRotation(t *testing.T) {
	tests := []struct {
		rotation int64
		want     int64
	}{
		{rotation: 0, want: 0},
		{rotation: 90, want: 90},
		{rotation: 180, want: 180},
		{rotation: 270, want: -90},
		{rotation: -90, want: -90},
		{rotation: 450, want: 90},
	}

	for _, test := range tests {
		if got := signedRotation(test.rotation); got != test.want {
			t.Errorf("signedRotation(%d) = %d, want %d", test.rotation, got, test.want)
		}
	}
}

func TestRotationContainerFor(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		probe      string
		wantFormat string
		wantExt    string
		wantStore  rotationStorage
		wantErr    bool
	}{
		{name: "Matroska", path: "video.mkv", probe: "matroska,webm", wantFormat: "matroska", wantExt: ".mkv", wantStore: rotationStorageMatroskaTag},
		{name: "MP4", path: "video.MP4", probe: "mov,mp4,m4a,3gp,3g2,mj2", wantFormat: "mp4", wantExt: ".mp4", wantStore: rotationStorageDisplayMatrix},
		{name: "M4V", path: "video.m4v", probe: "mov,mp4,m4a,3gp,3g2,mj2", wantFormat: "mp4", wantExt: ".m4v", wantStore: rotationStorageDisplayMatrix},
		{name: "QuickTime", path: "video.mov", probe: "mov,mp4,m4a,3gp,3g2,mj2", wantFormat: "mov", wantExt: ".mov", wantStore: rotationStorageDisplayMatrix},
		{name: "extension mismatch", path: "video.mp4", probe: "matroska,webm", wantErr: true},
		{name: "unsupported extension", path: "video.avi", probe: "avi", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := rotationContainerFor(test.path, test.probe)
			if test.wantErr {
				if err == nil {
					t.Fatal("rotationContainerFor unexpectedly succeeded")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.outputFormat != test.wantFormat || got.tempExtension != test.wantExt || got.storage != test.wantStore {
				t.Fatalf("rotationContainerFor() = %#v, want format=%q ext=%q storage=%d", got, test.wantFormat, test.wantExt, test.wantStore)
			}
		})
	}
}

func TestStageRotationMetadataAndRollback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "video.mkv")
	if err := os.WriteFile(path, []byte("original"), 0o640); err != nil {
		t.Fatal(err)
	}

	remuxer := &fakeRotationRemuxer{}
	prober := &fakeRotationProber{
		before: fakeRotationProbeResult{container: "matroska,webm", rotation: 180, tag: "180"},
		after:  fakeRotationProbeResult{container: "matroska,webm", rotation: 90, tag: "90"},
	}
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
	prober := &fakeRotationProber{
		before: fakeRotationProbeResult{container: "matroska,webm", rotation: 180, tag: "180"},
		after:  fakeRotationProbeResult{container: "matroska,webm"},
	}
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

func TestStageRotationMetadataMP4DisplayMatrix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	remuxer := &fakeRotationRemuxer{}
	prober := &fakeRotationProber{
		before: fakeRotationProbeResult{container: "mov,mp4,m4a,3gp,3g2,mj2"},
		after:  fakeRotationProbeResult{container: "mov,mp4,m4a,3gp,3g2,mj2", rotation: -90},
	}
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

	if patch.CurrentRotation != 0 || patch.Rotation != 270 {
		t.Fatalf("rotation patch = %d -> %d, want 0 -> 270", patch.CurrentRotation, patch.Rotation)
	}
	wantRotation := []string{"-display_rotation:v:0", "-90"}
	if !containsAdjacent(remuxer.args, wantRotation) {
		t.Fatalf("ffmpeg args %q do not contain %q", remuxer.args, wantRotation)
	}
	if indexOf(remuxer.args, "-display_rotation:v:0") > indexOf(remuxer.args, "-i") {
		t.Fatalf("display rotation must be an input option: %q", remuxer.args)
	}
	if indexOf(remuxer.args, "-metadata:s:v:0") != -1 {
		t.Fatalf("MP4 remux unexpectedly writes a Matroska ROTATE tag: %q", remuxer.args)
	}
	if !containsAdjacent(remuxer.args, []string{"-c", "copy"}) {
		t.Fatalf("ffmpeg args %q do not stream-copy media", remuxer.args)
	}
	if !containsAdjacent(remuxer.args, []string{"-f", "mp4"}) {
		t.Fatalf("ffmpeg args %q do not select the MP4 muxer", remuxer.args)
	}
	if filepath.Ext(patch.StagedPath) != ".mp4" {
		t.Fatalf("staged path %q does not retain the MP4 extension", patch.StagedPath)
	}
}

func TestStageRotationMetadataMOVClear(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "video.mov")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	remuxer := &fakeRotationRemuxer{}
	prober := &fakeRotationProber{
		before: fakeRotationProbeResult{container: "mov,mp4,m4a,3gp,3g2,mj2", rotation: 90},
		after:  fakeRotationProbeResult{container: "mov,mp4,m4a,3gp,3g2,mj2"},
	}
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

	if !containsAdjacent(remuxer.args, []string{"-display_rotation:v:0", "0"}) {
		t.Fatalf("ffmpeg args %q do not clear the display matrix", remuxer.args)
	}
	if !containsAdjacent(remuxer.args, []string{"-f", "mov"}) {
		t.Fatalf("ffmpeg args %q do not select the MOV muxer", remuxer.args)
	}
	if filepath.Ext(patch.StagedPath) != ".mov" {
		t.Fatalf("staged path %q does not retain the MOV extension", patch.StagedPath)
	}
}

func containsAdjacent(values, want []string) bool {
	for i := 0; i+len(want) <= len(values); i++ {
		if slices.Equal(values[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

func indexOf(values []string, want string) int {
	return slices.Index(values, want)
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
