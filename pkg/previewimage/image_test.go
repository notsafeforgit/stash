package previewimage

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stretchr/testify/require"
)

func imageTestEncoder(t *testing.T) (Encoder, *ffmpeg.FFProbe) {
	t.Helper()
	paths := make(map[string]string)
	for _, tool := range []string{"ffmpeg", "ffprobe", "avifenc", "avifdec", "avifgainmaputil"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s required for image encoding regression", tool)
		}
		paths[tool] = path
	}
	return Encoder{FFmpeg: ffmpeg.NewEncoder(paths["ffmpeg"]), AVIFTool: paths["avifenc"], GainMapTool: paths["avifgainmaputil"]}, ffmpeg.NewFFProbe(paths["ffprobe"])
}

func TestImageThumbnailsRetainHDR(t *testing.T) {
	e, probe := imageTestEncoder(t)
	dir := t.TempDir()
	// Produce a real PQ source, then feed both plain and adaptive AVIF stills
	// back through image generation. This catches decoding just the SDR base.
	videoPath := filepath.Join(dir, "hdr.mkv")
	out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=128x96:rate=1:duration=1",
		"-vf", "format=yuv420p10le,setparams=color_primaries=bt2020:color_trc=smpte2084:colorspace=bt2020nc", "-c:v", "ffv1", videoPath).CombinedOutput()
	require.NoError(t, err, string(out))
	video, err := probe.NewVideoFile(videoPath)
	require.NoError(t, err)
	for _, mode := range []string{"PQ AVIF", "gain-map AVIF", "gain-map JPEG"} {
		t.Run(mode, func(t *testing.T) {
			path := "testdata/ultrahdr.jpg"
			if mode != "gain-map JPEG" {
				encoder := e
				if mode == "PQ AVIF" {
					encoder.GainMapTool = ""
				}
				original, err := encoder.Generate(context.Background(), dir, Request{Video: video})
				require.NoError(t, err)
				defer original.Close()
				require.GreaterOrEqual(t, len(original.Variants), 2)
				path = filepath.Join(original.Directory, original.Variants[1].File)
			}
			source, err := probe.NewVideoFile(path)
			require.NoError(t, err)
			result, err := e.Generate(context.Background(), dir, Request{Video: source, StillImage: true, MaxDimension: 64})
			require.NoError(t, err)
			defer result.Close()
			require.Empty(t, result.Warnings)
			require.Len(t, result.Variants, 2)
			v := result.Variants[1]
			require.Equal(t, Adaptive, v.DynamicRange)
			require.LessOrEqual(t, max(v.Width, v.Height), 64)
			out, err := exec.Command("avifdec", "--info", filepath.Join(result.Directory, v.File)).CombinedOutput()
			require.NoError(t, err, string(out))
			require.True(t, strings.Contains(string(out), "Base Image is SDR") || strings.Contains(string(out), "Base Headroom 0.00 (SDR)"), string(out))
			require.Contains(t, string(out), "Bit Depth      : 10")
			require.Contains(t, string(out), "Transfer Char. : 16")
			assertSDRBase(t, result.Directory, result.Variants[0].File, v.File)
		})
	}
}

func TestAVIFGainMapMetadataVersions(t *testing.T) {
	for _, info := range []string{
		" * Gain map       : Present\n    * Base Image is SDR\n",
		" * Gain map       : 512x384 pixels, 8 bit, YUV400, Full Range, Matrix Coeffs. 6, Base Headroom 0.00 (SDR), Alternate Headroom 3.50 (HDR)\n",
	} {
		require.True(t, avifHasGainMap(info))
	}
	for _, info := range []string{" * Transfer Char. : 16\n", " * Gain map       : Absent\n", " * Gain map : \n"} {
		require.False(t, avifHasGainMap(info))
	}
}

func TestStillImageSDRAndSmallSources(t *testing.T) {
	e, probe := imageTestEncoder(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "small.png")
	out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=size=31x23", "-frames:v", "1", path).CombinedOutput()
	require.NoError(t, err, string(out))
	source, err := probe.NewVideoFile(path)
	require.NoError(t, err)
	result, err := e.Generate(context.Background(), dir, Request{Video: source, StillImage: true, MaxDimension: 640})
	require.NoError(t, err)
	defer result.Close()
	require.Empty(t, result.Warnings)
	require.Len(t, result.Variants, 2)
	for _, v := range result.Variants {
		require.Equal(t, SDR, v.DynamicRange)
		require.Equal(t, 31, v.Width)
		require.Equal(t, 23, v.Height)
	}
	entries, err := os.ReadDir(result.Directory)
	require.NoError(t, err)
	require.Len(t, entries, 2)
}

func TestStillImagePQHLGAndOrientation(t *testing.T) {
	e, probe := imageTestEncoder(t)
	dir := t.TempDir()
	png := filepath.Join(dir, "source.png")
	out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=size=129x95", "-frames:v", "1", png).CombinedOutput()
	require.NoError(t, err, string(out))
	for _, transfer := range []string{"16", "18"} {
		t.Run(transfer, func(t *testing.T) {
			path := filepath.Join(dir, transfer+".avif")
			out, err := exec.Command(e.AVIFTool, "--cicp", "9/"+transfer+"/9", "-d", "10", "-y", "444", "-s", "8", "--irot", "3", "--imir", "1", png, path).CombinedOutput()
			require.NoError(t, err, string(out))
			source, err := probe.NewVideoFile(path)
			require.NoError(t, err)
			result, err := e.Generate(context.Background(), dir, Request{Video: source, StillImage: true, MaxDimension: 64})
			require.NoError(t, err)
			defer result.Close()
			require.Empty(t, result.Warnings)
			require.Len(t, result.Variants, 2)
			require.Equal(t, Adaptive, result.Variants[1].DynamicRange)
			require.Equal(t, 64, result.Variants[1].Height)
			require.Less(t, result.Variants[1].Width, result.Variants[1].Height)
			assertSDRBase(t, result.Directory, result.Variants[0].File, result.Variants[1].File)
		})
	}
}

func TestGainMapJPEGOrientation(t *testing.T) {
	e, probe := imageTestEncoder(t)
	dir := t.TempDir()
	data, err := os.ReadFile("testdata/ultrahdr.jpg")
	require.NoError(t, err)
	// Change the fixture's existing orientation in place, leaving
	// its MPF gain-map offsets and all compressed pixels intact.
	start := bytes.Index(data, []byte("Exif\x00\x00")) + 6
	require.Greater(t, start, 5)
	exif := data[start:]
	var order binary.ByteOrder = binary.LittleEndian
	if string(exif[:2]) == "MM" {
		order = binary.BigEndian
	}
	ifd := int(order.Uint32(exif[4:8]))
	count := int(order.Uint16(exif[ifd:]))
	found := false
	for offset := ifd + 2; offset < ifd+2+12*count; offset += 12 {
		if order.Uint16(exif[offset:]) == 0x112 {
			order.PutUint16(exif[offset+8:], 6)
			found = true
			break
		}
	}
	require.True(t, found)
	path := filepath.Join(dir, "rotated.jpg")
	require.NoError(t, os.WriteFile(path, data, 0600))
	source, err := probe.NewVideoFile(path)
	require.NoError(t, err)
	result, err := e.Generate(context.Background(), dir, Request{Video: source, StillImage: true, MaxDimension: 64})
	require.NoError(t, err)
	defer result.Close()
	require.Empty(t, result.Warnings)
	require.Equal(t, Adaptive, result.Variants[1].DynamicRange)
	for _, v := range result.Variants {
		require.Equal(t, 64, v.Height)
		require.Less(t, v.Width, v.Height)
	}
	assertSDRBase(t, result.Directory, result.Variants[0].File, result.Variants[1].File)
}

func TestAVIFTransformsRejectInvalidGeometry(t *testing.T) {
	info := " * clap (Clean Aperture): W: 20/1\n * Valid, derived crop rect: X: 2, Y: 4, W: 20, H: 10\n * irot (Rotation): 1\n * imir (Mirror): 1 (left-to-right)"
	filters, err := avifTransformFilters(info, false)
	require.NoError(t, err)
	require.Equal(t, []string{"crop=20:10:2:4", "transpose=cclock", "hflip"}, filters)
	filters, err = avifTransformFilters(info, true)
	require.NoError(t, err)
	require.Empty(t, filters, "native crop/rotation/mirror must not be applied twice")
	filters, err = avifTransformFilters(info+"\n * pasp (Aspect Ratio): 2/1", true)
	require.NoError(t, err)
	require.Equal(t, []string{"scale=w=iw:h='max(1,round(ih*2/1))':flags=lanczos", "setsar=1"}, filters)
	for _, info := range []string{"* clap (Clean Aperture): invalid", "* pasp (Aspect Ratio): 1/0", "* irot (Rotation): 4", "* imir (Mirror): -1"} {
		for _, native := range []bool{false, true} {
			_, err := avifTransformFilters(info, native)
			require.Error(t, err)
		}
	}
}

func TestSDRPhotoDecoder(t *testing.T) {
	vips, err := exec.LookPath("vips")
	if err != nil {
		t.Skip("vips required for ICC photo decoder regression")
	}
	e, probe := imageTestEncoder(t)
	e.VIPSTool = vips
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=size=80x120", "-frames:v", "1", path).CombinedOutput()
	require.NoError(t, err, string(out))
	source, err := probe.NewVideoFile(path)
	require.NoError(t, err)
	result, err := e.Generate(context.Background(), dir, Request{Video: source, StillImage: true, MaxDimension: 64})
	require.NoError(t, err)
	defer result.Close()
	require.Empty(t, result.Warnings)
	require.Len(t, result.Variants, 2)
	require.Equal(t, SDR, result.Variants[1].DynamicRange)
	require.Equal(t, 64, result.Variants[1].Height)
}

func TestJPEGGainMapDetectionAndUnavailableTool(t *testing.T) {
	gain, err := jpegHasGainMap("testdata/ultrahdr.jpg")
	require.NoError(t, err)
	require.True(t, gain)
	e, probe := imageTestEncoder(t)
	source, err := probe.NewVideoFile("testdata/ultrahdr.jpg")
	require.NoError(t, err)
	e.GainMapTool = ""
	_, err = e.Generate(context.Background(), t.TempDir(), Request{Video: source, StillImage: true, MaxDimension: 64})
	require.ErrorContains(t, err, "HDR JPEG requires avifgainmaputil")
	for _, metadata := range []string{"", "Base headroom: NaN\nAlternate headroom: 4", "Base headroom: 0\nAlternate headroom: Inf", "Base headroom: 0\nAlternate headroom: -1"} {
		_, err := gainMapHeadroom(metadata)
		require.Error(t, err)
	}
	headroom, err := gainMapHeadroom("Base headroom: 0 (as fraction: 0/1)\nAlternate headroom: 4 (as fraction: 4/1)")
	require.NoError(t, err)
	require.Equal(t, 4.0, headroom)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	e.GainMapTool = "avifgainmaputil"
	_, err = e.Generate(cancelled, t.TempDir(), Request{Video: source, StillImage: true, MaxDimension: 64})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "context canceled"))
}
