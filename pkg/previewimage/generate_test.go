package previewimage

import (
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/ffmpeg"
)

func TestGenerateRenditions(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is required for the encoding regression")
	}
	probePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is required for the encoding regression")
	}
	encoder := ffmpeg.NewEncoder(ffmpegPath)
	probe := ffmpeg.NewFFProbe(probePath)
	gainTool, _ := exec.LookPath("avifgainmaputil")
	avifTool, _ := exec.LookPath("avifenc")
	// Minimal FFmpeg builds remain supported by the application; these
	// round-trip tests require its optional colour and AVIF implementations.
	filters, _ := exec.Command(ffmpegPath, "-hide_banner", "-filters").CombinedOutput()
	encoders, _ := exec.Command(ffmpegPath, "-hide_banner", "-encoders").CombinedOutput()
	if !strings.Contains(string(filters), " zscale ") {
		t.Skip("round-trip encoding requires FFmpeg zscale")
	}
	for _, transfer := range []string{"bt709", "smpte2084", "arib-std-b67"} {
		t.Run(transfer, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "source.mkv")
			primaries, matrix := "bt709", "bt709"
			if transfer != "bt709" {
				primaries, matrix = "bt2020", "bt2020nc"
			}
			out, err := exec.Command(ffmpegPath, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=128x96:rate=2:duration=2",
				"-vf", "format=yuv420p10le,setparams=color_primaries="+primaries+":color_trc="+transfer+":colorspace="+matrix, "-c:v", "ffv1", path).CombinedOutput()
			if err != nil {
				t.Fatalf("fixture: %v: %s", err, out)
			}
			video, err := probe.NewVideoFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if video.ColorTransfer != transfer {
				t.Fatalf("fixture lost transfer: %s", video.ColorTransfer)
			}
			for _, mode := range []string{"ffmpeg", "native", "gain-map"} {
				t.Run(mode, func(t *testing.T) {
					e := Encoder{FFmpeg: encoder}
					adaptive := mode == "gain-map"
					switch mode {
					case "ffmpeg":
						if !strings.Contains(string(encoders), "libaom-av1") {
							t.Skip("FFmpeg libaom-av1 is unavailable")
						}
					case "native":
						if avifTool == "" {
							t.Skip("avifenc is unavailable")
						}
						e.AVIFTool = avifTool
					case "gain-map":
						if gainTool == "" || transfer == "bt709" {
							t.Skip("gain maps require the utility and an HDR source")
						}
					}
					if adaptive {
						e.GainMapTool = gainTool
					}
					result, err := e.Generate(context.Background(), dir, Request{Video: video, At: 0.5, MaxDimension: 64})
					if err != nil {
						t.Fatal(err)
					}
					defer result.Close()
					if len(result.Warnings) != 0 || len(result.Variants) != 2 {
						t.Fatalf("missing AVIF: %+v; %v", result.Variants, result.Warnings)
					}
					if _, err := result.JPEG(); err != nil {
						t.Fatal(err)
					}
					variant := result.Variants[1]
					wantRange, wantTransfer := SDR, "iec61966-2-1"
					if IsHDR(transfer) {
						wantRange, wantTransfer = HDR, "smpte2084"
					}
					if adaptive {
						wantRange, wantTransfer = Adaptive, "iec61966-2-1"
						out, err := exec.Command(gainTool, "printmetadata", filepath.Join(result.Directory, variant.File)).CombinedOutput()
						if err != nil || !strings.Contains(string(out), "Base headroom:") || !strings.Contains(string(out), "0 (as fraction: 0/1)") {
							t.Fatalf("not an SDR-base gain map: %v: %s", err, out)
						}
						// Older ffprobe versions don't expose CICP on gain-map
						// image items. Validate the container with its native decoder.
						info, err := exec.Command("avifdec", "--info", filepath.Join(result.Directory, variant.File)).CombinedOutput()
						if err != nil || !strings.Contains(string(info), "Base Image is SDR") || !strings.Contains(string(info), "Transfer Char. : 16") || !strings.Contains(string(info), "Transfer Char. : 13") {
							t.Fatalf("invalid adaptive color metadata: %v: %s", err, info)
						}
						assertSDRBase(t, result)
					}
					if variant.DynamicRange != wantRange || variant.Width != 64 || variant.Height != 48 {
						t.Fatalf("incorrect rendition: %+v", variant)
					}
					if mode == "native" {
						// FFmpeg 7 doesn't read libavif's item-level CICP here.
						// Check the container with its native decoder instead.
						primaries, transferID, matrix := 1, 13, 6
						if IsHDR(transfer) {
							primaries, transferID, matrix = 9, 16, 9
						}
						info, err := exec.Command("avifdec", "--info", filepath.Join(result.Directory, variant.File)).CombinedOutput()
						if err != nil || !strings.Contains(string(info), fmt.Sprintf("Color Primaries: %d", primaries)) ||
							!strings.Contains(string(info), fmt.Sprintf("Transfer Char. : %d", transferID)) ||
							!strings.Contains(string(info), fmt.Sprintf("Matrix Coeffs. : %d", matrix)) {
							t.Fatalf("invalid native AVIF color metadata: %v: %s", err, info)
						}
					}
					out, err := exec.Command(probePath, "-v", "error", "-show_entries", "stream=pix_fmt,color_transfer", "-of", "json", filepath.Join(result.Directory, variant.File)).CombinedOutput()
					if err != nil {
						t.Fatalf("probe avif: %v: %s", err, out)
					}
					var metadata struct {
						Streams []struct {
							PixFmt   string `json:"pix_fmt"`
							Transfer string `json:"color_transfer"`
						} `json:"streams"`
					}
					if err := json.Unmarshal(out, &metadata); err != nil {
						t.Fatal(err)
					}
					if len(metadata.Streams) == 0 || metadata.Streams[0].PixFmt != "yuv420p10le" || (mode == "ffmpeg" && metadata.Streams[0].Transfer != wantTransfer) {
						t.Fatalf("lost bit depth or transfer: %s", out)
					}
					if _, err := os.Stat(filepath.Join(result.Directory, "hdr.png")); !os.IsNotExist(err) {
						t.Fatal("retained HDR intermediate")
					}
				})
			}
		})
	}
}

func assertSDRBase(t *testing.T, result *Result) {
	t.Helper()
	path := filepath.Join(result.Directory, "decoded-sdr.png")
	out, err := exec.Command("avifdec", "--depth", "8", filepath.Join(result.Directory, "preview.avif"), path).CombinedOutput()
	if err != nil {
		t.Fatalf("decode SDR base: %v: %s", err, out)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	base, err := png.Decode(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	f, err = os.Open(filepath.Join(result.Directory, "preview.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := jpeg.Decode(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if base.Bounds() != fallback.Bounds() {
		t.Fatal("SDR base and JPEG have different dimensions")
	}
	var difference float64
	for y := base.Bounds().Min.Y; y < base.Bounds().Max.Y; y++ {
		for x := base.Bounds().Min.X; x < base.Bounds().Max.X; x++ {
			r, g, b, _ := base.At(x, y).RGBA()
			r2, g2, b2, _ := fallback.At(x, y).RGBA()
			difference += math.Abs(float64(r)-float64(r2)) + math.Abs(float64(g)-float64(g2)) + math.Abs(float64(b)-float64(b2))
		}
	}
	// Account for independent AVIF/JPEG quantization and chroma subsampling;
	// a wrong transfer or matrix produces a visibly different SDR rendition.
	mean := difference / float64(base.Bounds().Dx()*base.Bounds().Dy()*3) / 257
	if mean > 12 {
		t.Fatalf("SDR base differs from tone-mapped JPEG: mean channel error %.2f/255", mean)
	}
}
