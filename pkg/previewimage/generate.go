// Package previewimage generates display-adaptive still images independently of
// the legacy scene screenshot, marker and sprite storage formats.
package previewimage

import (
	"context"
	"fmt"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	stashexec "github.com/stashapp/stash/pkg/exec"
	"github.com/stashapp/stash/pkg/ffmpeg"
)

type DynamicRange string

const (
	SDR      DynamicRange = "SDR"
	HDR      DynamicRange = "HDR"
	Adaptive DynamicRange = "ADAPTIVE"
)

type Variant struct {
	File         string       `json:"file"`
	MIMEType     string       `json:"mime_type"`
	DynamicRange DynamicRange `json:"dynamic_range"`
	Width        int          `json:"width"`
	Height       int          `json:"height"`
}

// Encoder keeps optional AVIF tooling out of callers and of the storage model.
// GainMapTool is an optional path to libavif's avifgainmaputil (>= 1.2).
// AVIFTool is an optional avifenc path for FFmpeg builds without libaom.
type Encoder struct {
	FFmpeg      *ffmpeg.FFMpeg
	GainMapTool string
	AVIFTool    string
}

type Request struct {
	Video *ffmpeg.VideoFile
	At    float64
	// MaxDimension bounds the longest edge; zero retains the source size.
	MaxDimension int
}

type Result struct {
	Directory string
	Variants  []Variant
	// Warnings describe optional encoders that failed. The SDR JPEG is always
	// complete when Generate succeeds, including when AVIF isn't available.
	Warnings []error
}

func IsHDR(transfer string) bool {
	return transfer == "smpte2084" || transfer == "arib-std-b67"
}

func (r *Result) Close() { _ = os.RemoveAll(r.Directory) }

func (r *Result) JPEG() ([]byte, error) {
	return os.ReadFile(filepath.Join(r.Directory, "preview.jpg"))
}

func (e Encoder) Generate(ctx context.Context, parent string, req Request) (_ *Result, err error) {
	if req.Video == nil || req.Video.Path == "" || req.At < 0 || math.IsNaN(req.At) || math.IsInf(req.At, 0) || req.MaxDimension < 0 {
		return nil, fmt.Errorf("invalid preview image request")
	}
	if err := os.MkdirAll(parent, 0755); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(parent, ".preview-*")
	if err != nil {
		return nil, err
	}
	ret := &Result{Directory: dir}
	defer func() {
		if err != nil {
			ret.Close()
		}
	}()

	// Both renditions come from the same decoded frame. All HDR processing
	// stays floating point / 16 bit until the final encoders, never Go's 8-bit
	// montage pipeline. A failed seek retries accurately without relabeling HDR
	// pixels as BT.709.
	err = e.extractFrame(ctx, req, dir, false)
	if err != nil && ctx.Err() == nil {
		err = e.extractFrame(ctx, req, dir, true)
	}
	if err != nil {
		return nil, err
	}
	width, height, err := writeJPEG(dir)
	if err != nil {
		return nil, err
	}
	ret.Variants = []Variant{{"preview.jpg", "image/jpeg", SDR, width, height}}

	hdr := IsHDR(req.Video.ColorTransfer)
	avif := filepath.Join(dir, "preview.avif")
	if hdr && e.GainMapTool != "" {
		args := []string{"combine", filepath.Join(dir, "sdr.png"), filepath.Join(dir, "hdr.png"), avif,
			"--cicp-base", "1/13/6", "--cicp-alternate", "9/16/9", "--ignore-profile",
			"-d", "10", "-y", "420", "-q", "80", "-s", "8", "--qgain-map", "80", "--downscaling", "2"}
		out, gainErr := stashexec.CommandContext(ctx, e.GainMapTool, args...).CombinedOutput()
		if gainErr == nil {
			ret.Variants = append(ret.Variants, Variant{"preview.avif", "image/avif", Adaptive, width, height})
		} else {
			ret.Warnings = append(ret.Warnings, fmt.Errorf("AVIF gain map: %w: %s", gainErr, out))
		}
	}
	if len(ret.Variants) == 1 && ctx.Err() == nil {
		// Plain HDR AVIF is still useful on HDR displays when libavif's gain
		// map utility is absent. The manifest marks it HDR-only so SDR clients
		// select the independently tone-mapped JPEG instead.
		var avifErr error
		if e.AVIFTool != "" {
			out, err := stashexec.CommandContext(ctx, e.AVIFTool, nativeAVIFArgs(dir, hdr)...).CombinedOutput()
			avifErr = err
			if err != nil {
				ret.Warnings = append(ret.Warnings, fmt.Errorf("AVIF encoder: %w: %s", err, out))
			}
		}
		if (e.AVIFTool == "" || avifErr != nil) && ctx.Err() == nil {
			avifErr = e.run(ctx, avifArgs(dir, hdr))
			if avifErr != nil {
				ret.Warnings = append(ret.Warnings, avifErr)
			}
		}
		if avifErr == nil {
			dr := SDR
			if hdr {
				dr = HDR
			}
			ret.Variants = append(ret.Variants, Variant{"preview.avif", "image/avif", dr, width, height})
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The intermediates are not published or retained in the generated cache.
	_ = os.Remove(filepath.Join(dir, "sdr.png"))
	_ = os.Remove(filepath.Join(dir, "hdr.png"))
	return ret, nil
}

func (e Encoder) run(ctx context.Context, args ffmpeg.Args) error {
	out, err := e.FFmpeg.Command(ctx, args).CombinedOutput()
	if err != nil {
		return fmt.Errorf("preview image ffmpeg: %w: %s", err, out)
	}
	return nil
}

func (e Encoder) extractFrame(ctx context.Context, req Request, dir string, slowSeek bool) error {
	// A seek can reach EOF with ffmpeg exiting successfully but emitting no
	// frame. Treat that as a failed fast seek, too. Remove partial outputs so
	// an accurate retry cannot accidentally pair frames from different seeks.
	files := []string{"sdr.png"}
	if IsHDR(req.Video.ColorTransfer) {
		files = append(files, "hdr.png")
	}
	for _, file := range files {
		_ = os.Remove(filepath.Join(dir, file))
	}
	if err := e.run(ctx, frameArgs(req, dir, slowSeek)); err != nil {
		return err
	}
	for _, file := range files {
		if stat, err := os.Stat(filepath.Join(dir, file)); err != nil || stat.Size() == 0 {
			return fmt.Errorf("ffmpeg emitted no preview frame")
		}
	}
	return nil
}

func frameArgs(req Request, dir string, slowSeek bool) ffmpeg.Args {
	args := ffmpeg.Args{"-hide_banner", "-loglevel", "error", "-y", "-threads", "2"}
	if !slowSeek {
		args = args.Seek(req.At)
	}
	args = args.Input(req.Video.Path)
	var filters []string
	if slowSeek {
		// Apply accurate seek before the split so both outputs use one frame.
		filters = append(filters, fmt.Sprintf("trim=start=%f", req.At), "setpts=PTS-STARTPTS")
	}
	// Still-image consumers display square pixels. Bake anamorphic sample
	// aspect ratio into the dimensions before bounding the display size.
	filters = append(filters, "scale=w='max(1,round(iw*sar))':h=ih:flags=lanczos", "setsar=1")
	if req.MaxDimension > 0 {
		filters = append(filters, fmt.Sprintf("scale=w='min(iw,%d)':h='min(ih,%d)':force_original_aspect_ratio=decrease:flags=lanczos", req.MaxDimension, req.MaxDimension))
	}
	if IsHDR(req.Video.ColorTransfer) {
		// Fill only missing tags. HDR detection is based on transfer, never bit
		// depth alone. Normalize HLG to PQ for interoperable HDR still images.
		primaries := colorTag(req.Video.ColorPrimaries, "bt2020")
		matrix := colorTag(req.Video.ColorSpace, "bt2020nc")
		rangeTag := colorTag(req.Video.ColorRange, "limited")
		filters = append(filters, fmt.Sprintf("setparams=color_primaries=%s:color_trc=%s:colorspace=%s:range=%s", primaries, req.Video.ColorTransfer, matrix, rangeTag))
		graph := "[0:v:0]" + strings.Join(filters, ",") + ",split=2[sdrin][hdrin];" +
			"[sdrin]zscale=t=linear:npl=203,format=gbrpf32le,zscale=p=bt709,tonemap=mobius:param=0.3:desat=2,zscale=t=iec61966-2-1:r=full,format=gbrp16le,format=rgb48be[sdr];" +
			"[hdrin]zscale=p=bt2020:t=smpte2084:r=full,format=gbrp16le,format=rgb48be[hdr]"
		args = append(args, "-filter_complex_threads", "1", "-filter_complex", graph,
			"-map", "[sdr]", "-frames:v", "1", "-update", "1", filepath.Join(dir, "sdr.png"),
			"-map", "[hdr]", "-frames:v", "1", "-update", "1", filepath.Join(dir, "hdr.png"))
	} else {
		filters = append(filters, "format=rgb48be")
		args = append(args, "-map", "0:v:0", "-vf", strings.Join(filters, ","), "-frames:v", "1", "-update", "1", filepath.Join(dir, "sdr.png"))
	}
	return args
}

func colorTag(value, fallback string) string {
	switch value {
	case "", "unknown", "unspecified", "reserved":
		return fallback
	default:
		return value
	}
}

func avifArgs(dir string, hdr bool) ffmpeg.Args {
	input, primaries, transfer, matrix := "sdr.png", "bt709", "iec61966-2-1", "bt470bg"
	scaleMatrix := "bt601"
	if hdr {
		input, primaries, transfer, matrix = "hdr.png", "bt2020", "smpte2084", "bt2020nc"
		scaleMatrix = "bt2020"
	}
	return ffmpeg.Args{"-hide_banner", "-loglevel", "error", "-y", "-i", filepath.Join(dir, input),
		"-vf", "scale=out_color_matrix=" + scaleMatrix + ":out_range=full,format=yuv420p10le,setparams=color_primaries=" + primaries + ":color_trc=" + transfer + ":colorspace=" + matrix + ":range=full",
		"-frames:v", "1", "-c:v", "libaom-av1", "-still-picture", "1", "-cpu-used", "8", "-crf", "22", "-b:v", "0", "-threads", "2",
		"-color_primaries", primaries, "-color_trc", transfer, "-colorspace", matrix, "-color_range", "pc", "-f", "avif", filepath.Join(dir, "preview.avif")}
}

func nativeAVIFArgs(dir string, hdr bool) []string {
	input, cicp := "sdr.png", "1/13/6"
	if hdr {
		input, cicp = "hdr.png", "9/16/9"
	}
	return []string{"--ignore-icc", "--cicp", cicp, "--range", "full", "-d", "10", "-y", "420", "-q", "80", "-s", "8", "-j", "2",
		filepath.Join(dir, input), filepath.Join(dir, "preview.avif")}
}

func writeJPEG(dir string) (int, int, error) {
	f, err := os.Open(filepath.Join(dir, "sdr.png"))
	if err != nil {
		return 0, 0, err
	}
	im, err := png.Decode(f)
	_ = f.Close()
	if err != nil {
		return 0, 0, err
	}
	f, err = os.Create(filepath.Join(dir, "preview.jpg"))
	if err != nil {
		return 0, 0, err
	}
	err = jpeg.Encode(f, im, &jpeg.Options{Quality: 90})
	closeErr := f.Close()
	if err != nil {
		return 0, 0, err
	}
	return im.Bounds().Dx(), im.Bounds().Dy(), closeErr
}
