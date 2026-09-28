package manager

import (
	"archive/zip"
	"bytes"
	"context"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/hash/md5"
	"github.com/stashapp/stash/pkg/image"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/previewimage"
	"github.com/stretchr/testify/require"
)

func TestImageThumbnailBackfillAndIdentity(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe", "avifenc"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s required", tool)
		}
	}
	mgr, _ := coverTestManager(t)
	mgr.FFMpeg, mgr.FFProbe = ffmpeg.NewEncoder("ffmpeg"), ffmpeg.NewFFProbe("ffprobe")
	previous := instance
	instance = mgr
	t.Cleanup(func() { instance = previous })
	path := filepath.Join(t.TempDir(), "small.png")
	out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=size=32x24", "-frames:v", "1", path).CombinedOutput()
	require.NoError(t, err, string(out))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	f := &models.ImageFile{BaseFile: &models.BaseFile{ID: 1, Path: path}, Format: "png", Width: 32, Height: 24}
	img := models.Image{ID: 1, Path: path, Checksum: md5.FromBytes(data), Files: models.NewRelatedFiles([]models.File{f})}
	legacy := mgr.Paths.Generated.GetThumbnailPath(img.Checksum, models.DefaultGthumbWidth)
	require.NoError(t, os.MkdirAll(filepath.Dir(legacy), 0755))
	require.NoError(t, os.WriteFile(legacy, []byte("old thumbnail"), 0600))
	task := GenerateImageThumbnailTask{Image: img}
	// Existing JPEGs and small HDR-capable originals must not block backfill.
	require.True(t, task.required())
	var taskErr error
	task.onError = func(err error) { taskErr = err }
	task.Start(context.Background())
	require.NoError(t, taskErr)
	m := mgr.ImagePreviewImage(&img)
	require.NotNil(t, m)
	require.Len(t, m.Variants, 1)
	require.Equal(t, "image/avif", m.Variants[0].MIMEType)
	require.Equal(t, 32, m.Variants[0].Width)
	require.False(t, task.required())
	compatibility, err := os.ReadFile(legacy)
	require.NoError(t, err)
	jpegConfig, err := jpeg.DecodeConfig(bytes.NewReader(compatibility))
	require.NoError(t, err)
	require.Equal(t, 32, jpegConfig.Width)
	asset, _ := mgr.PreviewImageStore().File(img.ID, "image", m, m.Variants[0].File)
	duplicates, err := filepath.Glob(filepath.Join(filepath.Dir(asset), "*.jpg"))
	require.NoError(t, err)
	require.Empty(t, duplicates, "v3 must not duplicate the legacy JPEG")
	require.NoError(t, os.Remove(legacy))
	require.True(t, task.required(), "repair a missing legacy fallback even with a valid AVIF")
	// The same numeric scene ID cannot alias an image rendition.
	_, err = mgr.PreviewImageStore().Load(img.ID, "cover", previewimage.ImageKey(img.Checksum))
	require.Error(t, err)
	changed := img
	changed.Checksum = "replacement"
	require.Nil(t, mgr.ImagePreviewImage(&changed))
	mgr.Config.SetBool(config.EnableV3UI, false)
	require.Nil(t, mgr.ImagePreviewImage(&img))
	mgr.Config.SetBool(config.EnableV3UI, true)
	require.NoError(t, os.Remove(f.Base().Path))
	require.NotNil(t, mgr.ImagePreviewImage(&img), "offline originals should retain existing thumbnails")
	task.Overwrite = true
	task.Start(context.Background())
	require.Error(t, taskErr)
	require.NotNil(t, mgr.ImagePreviewImage(&img), "failed replacement must keep existing renditions")
	f.Format = "gif"
	require.ErrorIs(t, mgr.GenerateImagePreview(context.Background(), &img), image.ErrNotSupportedForThumbnail)
}

func TestImageThumbnailsInArchivesAndAnimatedOriginals(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe", "avifenc"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s required", tool)
		}
	}
	mgr, _ := coverTestManager(t)
	mgr.FFMpeg, mgr.FFProbe = ffmpeg.NewEncoder("ffmpeg"), ffmpeg.NewFFProbe("ffprobe")
	dir := t.TempDir()
	path := filepath.Join(dir, "source.png")
	out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=size=80x60", "-frames:v", "1", path).CombinedOutput()
	require.NoError(t, err, string(out))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	archivePath := filepath.Join(dir, "gallery.zip")
	archive, err := os.Create(archivePath)
	require.NoError(t, err)
	writer := zip.NewWriter(archive)
	member, err := writer.Create("folder/source.png")
	require.NoError(t, err)
	_, err = member.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, archive.Close())
	stat, err := os.Stat(archivePath)
	require.NoError(t, err)
	f := &models.ImageFile{BaseFile: &models.BaseFile{ID: 1, Path: filepath.Join(archivePath, "folder", "source.png"),
		ZipFile: &models.BaseFile{Path: archivePath, Size: stat.Size()}}, Format: "png", Width: 80, Height: 60}
	img := &models.Image{ID: 1, Checksum: md5.FromBytes(data), Files: models.NewRelatedFiles([]models.File{f})}
	require.NoError(t, mgr.GenerateImagePreview(context.Background(), img))
	require.NotNil(t, mgr.ImagePreviewImage(img))
	stages, err := filepath.Glob(filepath.Join(mgr.PreviewImageStore().Root, ".image-source-*"))
	require.NoError(t, err)
	require.Empty(t, stages, "archive members must not be retained")
	for _, format := range []string{"webp", "apng"} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(dir, "animated."+format)
			out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=size=32x24:rate=2:duration=1", "-plays", "0", path).CombinedOutput()
			require.NoError(t, err, string(out))
			f := &models.ImageFile{BaseFile: &models.BaseFile{ID: 2, Path: path}, Format: format, Width: 32, Height: 24}
			img := &models.Image{ID: 2, Checksum: "animated", Files: models.NewRelatedFiles([]models.File{f})}
			require.ErrorIs(t, mgr.GenerateImagePreview(context.Background(), img), image.ErrNotSupportedForThumbnail)
			require.Nil(t, mgr.ImagePreviewImage(img), "animation must not become a static thumbnail")
		})
	}
}

func TestImageGenerationFailuresAreBounded(t *testing.T) {
	var failures imageGenerationFailures
	require.NoError(t, failures.Err())
	for range 3 {
		failures.Add(os.ErrNotExist)
	}
	require.ErrorContains(t, failures.Err(), "3 image thumbnails could not be generated")
}
