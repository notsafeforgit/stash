package ingest_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/file"
	imagefile "github.com/stashapp/stash/pkg/file/image"
	"github.com/stashapp/stash/pkg/file/video"
	"github.com/stashapp/stash/pkg/hash/md5"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

type intakeFingerprinter struct{}

func (intakeFingerprinter) CalculateFingerprints(f *models.BaseFile, opener file.Opener, useExisting bool) ([]models.Fingerprint, error) {
	r, err := opener.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	digest, err := md5.FromReader(r)
	if err != nil {
		return nil, err
	}
	return []models.Fingerprint{{Type: models.FingerprintTypeMD5, Fingerprint: digest}}, nil
}

func intakeProbe(t *testing.T) *ffmpeg.FFProbe {
	t.Helper()
	path, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	return ffmpeg.NewFFProbe(path)
}

func intakeRoot(t *testing.T, name string, body []byte) (models.MediaRoot, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, body, 0600))
	binding, err := archive.ProbeMediaRoot(dir)
	require.NoError(t, err)
	return models.MediaRoot{UUID: "test-root", MediaRootDefinition: models.MediaRootDefinition{State: "active", Binding: binding}}, path
}

func intakePNG(t *testing.T) []byte {
	t.Helper()
	var content bytes.Buffer
	require.NoError(t, png.Encode(&content, image.NewRGBA(image.Rect(0, 0, 17, 23))))
	return content.Bytes()
}

func TestPrepareMediaUsesSharedScannerWithoutDatabaseWrites(t *testing.T) {
	scanner := &file.Scanner{FingerprintCalculator: intakeFingerprinter{}, FileDecorators: []file.Decorator{&imagefile.Decorator{FFProbe: intakeProbe(t)}}}
	body := intakePNG(t)
	root, path := intakeRoot(t, "purchased.png", body)
	size := int64(len(body))
	prepared, err := ingest.PrepareMedia(t.Context(), root, "purchased.png", &size, ingest.Digest(body), scanner)
	require.NoError(t, err)
	defer prepared.Close()
	media := prepared.File().(*models.ImageFile)
	require.Equal(t, path, media.Path)
	require.Equal(t, 17, media.Width)
	require.Equal(t, 23, media.Height)
	require.Zero(t, media.ID, "preparation must not persist a file or publish a receipt")
	require.Zero(t, media.ParentFolderID)
	expectedMD5, err := md5.FromReader(bytes.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, expectedMD5, media.Fingerprints.Get(models.FingerprintTypeMD5))
	require.Equal(t, ingest.Digest(body), prepared.SHA256())
	require.Equal(t, size, prepared.Snapshot().Size)
	require.NoError(t, prepared.Revalidate(t.Context(), root))
	media.Fingerprints[0].Fingerprint = "caller modification"
	require.Equal(t, expectedMD5, prepared.File().Base().Fingerprints.Get(models.FingerprintTypeMD5))
	require.NoError(t, os.WriteFile(path, []byte("changed after preparation"), 0600))
	require.ErrorIs(t, prepared.Revalidate(t.Context(), root), archive.ErrMediaFileChanged)
}

type intakeMutatingDecorator struct {
	file.Decorator
	mutate func()
	after  func(models.File)
}

func (d intakeMutatingDecorator) Decorate(ctx context.Context, fs models.FS, input models.File) (models.File, error) {
	d.mutate()
	result, err := d.Decorator.Decorate(ctx, fs, input)
	if err == nil {
		d.after(result)
	}
	return result, err
}

func TestPrepareMediaRejectsReplacementBetweenHashAndProbe(t *testing.T) {
	root, path := intakeRoot(t, "image.png", intakePNG(t))
	inspectedOriginal := false
	decorator := intakeMutatingDecorator{
		Decorator: &imagefile.Decorator{FFProbe: intakeProbe(t)},
		mutate: func() {
			require.NoError(t, os.Rename(path, path+"-old"))
			require.NoError(t, os.WriteFile(path, []byte("replacement is not an image"), 0600))
		},
		after: func(f models.File) {
			image := f.(*models.ImageFile)
			require.Equal(t, 17, image.Width)
			require.Equal(t, 23, image.Height)
			inspectedOriginal = true
		},
	}
	scanner := &file.Scanner{FingerprintCalculator: intakeFingerprinter{}, FileDecorators: []file.Decorator{decorator}}
	prepared, err := ingest.PrepareMedia(t.Context(), root, "image.png", nil, "", scanner)
	require.ErrorIs(t, err, archive.ErrMediaFileChanged)
	require.Nil(t, prepared)
	require.True(t, inspectedOriginal, "the decorator must probe the hashed descriptor, not the replacement pathname")
}

func TestPrepareMediaPreservesAnimatedImageClassification(t *testing.T) {
	palette := color.Palette{color.Black, color.White}
	first := image.NewPaletted(image.Rect(0, 0, 8, 12), palette)
	second := image.NewPaletted(image.Rect(0, 0, 8, 12), palette)
	second.SetColorIndex(0, 0, 1)
	var body bytes.Buffer
	require.NoError(t, gif.EncodeAll(&body, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{10, 10}}))
	root, _ := intakeRoot(t, "animated.gif", body.Bytes())
	scanner := &file.Scanner{FingerprintCalculator: intakeFingerprinter{}, FileDecorators: []file.Decorator{&imagefile.Decorator{FFProbe: intakeProbe(t)}}}
	prepared, err := ingest.PrepareMedia(t.Context(), root, "animated.gif", nil, "", scanner)
	require.NoError(t, err)
	defer prepared.Close()
	clip := prepared.File().(*models.VideoFile)
	require.Equal(t, "gif", clip.VideoCodec)
	require.Equal(t, 8, clip.Width)
	require.Equal(t, 12, clip.Height)
	require.InDelta(t, 0.2, clip.Duration, 0.05)
}

func TestPrepareMediaRejectsAudioOnlyAndUnfinishedDownloads(t *testing.T) {
	probe := intakeProbe(t)
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	root, path := intakeRoot(t, "audio.mp4", nil)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, ffmpegPath, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=8000", "-t", "0.2", "-c:a", "aac", "-y", path).CombinedOutput()
	require.NoError(t, err, string(output))
	scanner := &file.Scanner{FingerprintCalculator: intakeFingerprinter{}, FileDecorators: []file.Decorator{&video.Decorator{FFProbe: probe}}}
	prepared, err := ingest.PrepareMedia(ctx, root, "audio.mp4", nil, "", scanner)
	require.ErrorIs(t, err, video.ErrNoVideoStream)
	require.Nil(t, prepared)
	require.NoError(t, os.Rename(path, path+".part"))
	_, err = ingest.PrepareMedia(ctx, root, "audio.mp4.part", nil, "", scanner)
	require.Error(t, err)
	_, err = ingest.PrepareMedia(ctx, root, "../outside.mp4", nil, "", scanner)
	require.Error(t, err)
	_, err = ingest.PrepareMedia(ctx, root, "audio.mp4", nil, "", &file.Scanner{})
	require.ErrorContains(t, err, "configured scanner")
}
