package ffmpeg

import (
	"context"
	"fmt"
	"os"
	"strings"
)

var bt709ColorMetadataBitstreamFilters = []string{
	"h264_metadata=colour_primaries=1:transfer_characteristics=1:matrix_coefficients=1",
	"hevc_metadata=colour_primaries=1:transfer_characteristics=1:matrix_coefficients=1",
}

func IsInvalidColorSpaceError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Invalid color space")
}

func ColorMetadataBitstreamFilter(codec string) (string, bool) {
	switch strings.ToLower(codec) {
	case "h264", "avc", "avc1":
		return bt709ColorMetadataBitstreamFilters[0], true
	case "h265", "hevc", "hev1", "hvc1":
		return bt709ColorMetadataBitstreamFilters[1], true
	default:
		return "", false
	}
}

func (f *FFMpeg) CreateColorMetadataFixedInput(ctx context.Context, input string, codec string) (string, func(), error) {
	filters := bt709ColorMetadataBitstreamFilters
	if bitstreamFilter, ok := ColorMetadataBitstreamFilter(codec); ok {
		filters = []string{bitstreamFilter}
	}

	var errs []string
	for _, bitstreamFilter := range filters {
		tmpPath, cleanup, err := f.createColorMetadataFixedInput(ctx, input, bitstreamFilter)
		if err == nil {
			return tmpPath, cleanup, nil
		}
		errs = append(errs, err.Error())
	}

	return "", nil, fmt.Errorf("rewriting video color metadata with ffmpeg: %s", strings.Join(errs, "; "))
}

func (f *FFMpeg) createColorMetadataFixedInput(ctx context.Context, input string, bitstreamFilter string) (string, func(), error) {
	tmp, err := os.CreateTemp("", "stash-color-metadata-*.mp4")
	if err != nil {
		return "", nil, fmt.Errorf("creating temporary color metadata file: %w", err)
	}

	tmpPath := tmp.Name()
	cleanup := func() {
		_ = os.Remove(tmpPath)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("closing temporary color metadata file: %w", err)
	}

	args := []string{
		"-v", "error",
		"-y",
		"-i", input,
		"-map", "0:v:0",
		"-c:v", "copy",
		"-an",
		"-bsf:v", bitstreamFilter,
		"-f", "mp4",
		tmpPath,
	}
	cmd := f.Command(ctx, args)
	output, err := cmd.CombinedOutput()
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}

	return tmpPath, cleanup, nil
}
