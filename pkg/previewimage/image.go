package previewimage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	stashexec "github.com/stashapp/stash/pkg/exec"
	"github.com/stashapp/stash/pkg/ffmpeg"
)

// extractImage feeds the same 16-bit SDR/HDR intermediates and encoders as
// scene frames. Gain-map sources must be reconstructed before resizing: their
// ordinary decoded base can be SDR even though the image is HDR.
func (e Encoder) extractImage(ctx context.Context, req Request, dir string) (bool, error) {
	video := *req.Video
	req.Video = &video
	ext := strings.ToLower(filepath.Ext(video.Path))
	avif := ext == ".avif" || video.VideoCodec == "av1"
	gainJPEG, err := jpegHasGainMap(video.Path)
	if err != nil {
		return false, err
	}
	input := video.Path
	if gainJPEG {
		if e.GainMapTool == "" {
			return false, fmt.Errorf("HDR JPEG requires avifgainmaputil")
		}
		input = filepath.Join(dir, "source.avif")
		defer os.Remove(input)
		if err := e.gainMapCommand(ctx, "convert", video.Path, input, "-q", "100", "--qgain-map", "100", "-s", "8"); err != nil {
			return false, err
		}
		avif = true
	}
	if avif {
		// Read container CICP and display transforms even on FFmpeg builds
		// that only understand the AV1 bitstream, not AVIF item properties.
		decoder, err := exec.LookPath("avifdec")
		if err != nil {
			return false, fmt.Errorf("AVIF image metadata requires avifdec: %w", err)
		}
		out, err := stashexec.CommandContext(ctx, decoder, "-j", "2", "--info", input).CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("reading AVIF metadata: %w: %s", err, out)
		}
		info := string(out)
		req.imageFilters, err = avifTransformFilters(info, false)
		if err != nil {
			return false, err
		}
		if avifHasGainMap(info) {
			if e.GainMapTool == "" {
				return false, fmt.Errorf("HDR AVIF gain map requires avifgainmaputil")
			}
			return true, e.extractGainMap(ctx, input, req, dir)
		}
		applyAVIFColor(&video, info)
		if !IsHDR(video.ColorTransfer) && e.VIPSTool != "" {
			return false, e.extractSDRPhoto(ctx, req, dir)
		}
		if len(req.imageFilters) > 0 {
			// Since 1.4 avifdec bakes crop/rotation/mirroring into PNG pixels.
			// Gain-map tonemap outputs above still need the explicit filters.
			version, err := stashexec.CommandContext(ctx, decoder, "--version").CombinedOutput()
			if err != nil {
				return false, fmt.Errorf("reading AVIF decoder version: %w: %s", err, version)
			}
			var major, minor int
			if _, err := fmt.Sscanf(string(version), "Version: %d.%d", &major, &minor); err != nil {
				return false, fmt.Errorf("unrecognized AVIF decoder version: %s", version)
			}
			if major > 1 || (major == 1 && minor >= 4) {
				req.imageFilters, err = avifTransformFilters(info, true)
				if err != nil {
					return false, err
				}
			}
		}
		decoded := filepath.Join(dir, "source-decoded.png")
		defer os.Remove(decoded)
		out, err = stashexec.CommandContext(ctx, decoder, "-j", "2", "-d", "16", "--png-compress", "0", input, decoded).CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("decoding AVIF image: %w: %s", err, out)
		}
		video.Path, video.ColorSpace, video.ColorRange = decoded, "gbr", "pc"
		req.rawImage = true
	} else if !IsHDR(video.ColorTransfer) && e.VIPSTool != "" {
		return false, e.extractSDRPhoto(ctx, req, dir)
	}
	return IsHDR(video.ColorTransfer), e.extractFrame(ctx, req, dir, false)
}

func avifHasGainMap(info string) bool {
	// libavif 1.4 reports headroom on a single "Gain map" line; older
	// versions describe the base image separately. Neither is plain AVIF.
	if strings.Contains(info, "Base Image is") {
		return true
	}
	for _, line := range strings.Split(info, "\n") {
		label, value, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*")), ":")
		if ok && strings.TrimSpace(label) == "Gain map" {
			return strings.TrimSpace(value) != "" && strings.TrimSpace(value) != "Absent"
		}
	}
	return false
}

func (e Encoder) extractSDRPhoto(ctx context.Context, req Request, dir string) error {
	bound := req.MaxDimension
	if bound == 0 {
		bound = max(req.Video.Width, req.Video.Height)
	}
	// Keep the existing photo decoder's EXIF/ICC handling, but hand its
	// lossless sRGB pixels to the same AVIF/JPEG encoders as scene covers.
	out, err := stashexec.CommandContext(ctx, e.VIPSTool, "thumbnail", req.Video.Path,
		filepath.Join(dir, "sdr.png")+"[bitdepth=16,strip]", strconv.Itoa(bound),
		"--height", strconv.Itoa(bound), "--size", "down", "--output-profile", "srgb").CombinedOutput()
	if err != nil {
		return fmt.Errorf("decoding SDR image: %w: %s", err, out)
	}
	return nil
}

func (e Encoder) extractGainMap(ctx context.Context, input string, req Request, dir string) error {
	out, err := stashexec.CommandContext(ctx, e.GainMapTool, "printmetadata", input).CombinedOutput()
	if err != nil {
		return fmt.Errorf("reading AVIF gain map: %w: %s", err, out)
	}
	headroom, err := gainMapHeadroom(string(out))
	if err != nil {
		return err
	}
	for _, image := range []struct{ name, headroom, cicp string }{
		{"sdr", "0", "1/13/6"}, {"hdr", strconv.FormatFloat(headroom, 'g', -1, 64), "9/16/9"},
	} {
		decoded := filepath.Join(dir, image.name+"-decoded.png")
		defer os.Remove(decoded)
		if err := e.gainMapCommand(ctx, "tonemap", input, decoded, "--headroom", image.headroom,
			"--cicp-output", image.cicp, "-d", "12"); err != nil {
			return err
		}
		filters := append([]string{"format=rgb48be"}, req.imageFilters...)
		if req.MaxDimension > 0 {
			filters = append(filters, boundedScale(req.MaxDimension))
		}
		if err := e.run(ctx, ffmpeg.Args{"-v", "error", "-y", "-noautorotate", "-i", decoded, "-vf", strings.Join(filters, ","),
			"-frames:v", "1", "-update", "1", filepath.Join(dir, image.name+".png")}); err != nil {
			return err
		}
	}
	return nil
}

func (e Encoder) gainMapCommand(ctx context.Context, args ...string) error {
	out, err := stashexec.CommandContext(ctx, e.GainMapTool, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("image gain map: %w: %s", err, out)
	}
	return nil
}

func gainMapHeadroom(metadata string) (float64, error) {
	var headroom float64
	var found int
	for _, line := range strings.Split(metadata, "\n") {
		if !strings.Contains(line, "Base headroom:") && !strings.Contains(line, "Alternate headroom:") {
			continue
		}
		_, value, _ := strings.Cut(line, ":")
		fields := strings.Fields(value)
		if len(fields) == 0 {
			return 0, fmt.Errorf("missing gain-map headroom")
		}
		n, err := strconv.ParseFloat(fields[0], 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 32 {
			return 0, fmt.Errorf("invalid gain-map headroom")
		}
		headroom = math.Max(headroom, n)
		found++
	}
	if found != 2 || headroom <= 0 {
		return 0, fmt.Errorf("missing HDR gain-map headroom")
	}
	return headroom, nil
}

func applyAVIFColor(video *ffmpeg.VideoFile, info string) {
	for _, line := range strings.Split(info, "\n") {
		label, value, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*")), ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "2" { // CICP's unspecified value, not a usable colour space.
			value = ""
		}
		switch strings.TrimSpace(label) {
		case "Color Primaries":
			video.ColorPrimaries = value
		case "Transfer Char.":
			video.ColorTransfer = map[string]string{"1": "bt709", "13": "iec61966-2-1", "16": "smpte2084", "18": "arib-std-b67"}[value]
		case "Matrix Coeffs.":
			video.ColorSpace = value
		case "Range":
			video.ColorRange = map[string]string{"Full": "pc", "Limited": "tv"}[value]
		}
	}
}

// Inspect JPEG metadata segments only, never scan or buffer the compressed
// image. Both Adobe Ultra HDR and ISO gain-map metadata precede the first SOS.
func jpegHasGainMap(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil || header != [2]byte{0xff, 0xd8} {
		return false, nil
	}
	for {
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return false, err
		}
		if header[0] != 0xff {
			return false, fmt.Errorf("invalid JPEG metadata marker")
		}
		for header[1] == 0xff {
			header[1], err = r.ReadByte()
			if err != nil {
				return false, err
			}
		}
		if header[1] == 0xda || header[1] == 0xd9 {
			return false, nil
		}
		if header[1] == 0x01 || (header[1] >= 0xd0 && header[1] <= 0xd7) {
			continue
		}
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return false, err
		}
		size := int(binary.BigEndian.Uint16(header[:])) - 2
		if size < 0 {
			return false, fmt.Errorf("invalid JPEG metadata length")
		}
		segment := make([]byte, size)
		if _, err := io.ReadFull(r, segment); err != nil {
			return false, err
		}
		if bytes.Contains(segment, []byte("hdr-gain-map")) || bytes.Contains(segment, []byte("hdrgm:")) || bytes.Contains(segment, []byte("urn:iso:std:iso:ts:21496")) {
			return true, nil
		}
	}
}
