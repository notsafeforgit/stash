package entityimage

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/stashapp/stash/pkg/ffmpeg"
)

var ErrUnsupportedFormat = errors.New("unsupported entity image format")

var DefaultGroupFrontImage = []byte{
	82, 73, 70, 70, 34, 0, 0, 0, 87, 69, 66, 80, 86, 80, 56, 32,
	22, 0, 0, 0, 48, 1, 0, 157, 1, 42, 1, 0, 1, 0, 14, 192,
	254, 37, 164, 0, 3, 112, 0, 0, 0, 0,
}

type NormalizeOptions struct {
	AllowHEIC bool
}

func Normalize(ctx context.Context, encoder *ffmpeg.FFMpeg, imageData []byte, options NormalizeOptions) ([]byte, error) {
	if len(imageData) == 0 {
		return nil, nil
	}

	if IsHEIC(imageData) && !options.AllowHEIC {
		return nil, fmt.Errorf("%w: HEIC/HEIF images are not supported for v3 entity images; use JPEG, PNG, WebP, GIF, or AVIF", ErrUnsupportedFormat)
	}

	if IsWebP(imageData) {
		return imageData, nil
	}

	if encoder == nil {
		return nil, fmt.Errorf("converting entity image to WebP: ffmpeg is not configured")
	}

	tmp, err := os.CreateTemp("", "stash-entity-image-*")
	if err != nil {
		return nil, fmt.Errorf("creating temporary image file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(imageData); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("writing temporary image file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("closing temporary image file: %w", err)
	}

	var args ffmpeg.Args
	args = append(args, "-hide_banner")
	args = args.LogLevel(ffmpeg.LogLevelError).
		Overwrite().
		Input(tmpName).
		VideoFrames(1).
		VideoCodec(ffmpeg.VideoCodecLibWebP)
	args = append(args, "-quality", "85", "-f", "webp", "-")

	webp, err := encoder.GenerateOutput(ctx, args, nil)
	if err != nil {
		return nil, fmt.Errorf("converting entity image to WebP: %w", err)
	}

	return webp, nil
}

func IsHEIC(data []byte) bool {
	if len(data) < 12 || string(data[4:8]) != "ftyp" {
		return false
	}

	for i := 8; i+4 <= len(data) && i < 64; i += 4 {
		switch string(data[i : i+4]) {
		case "heic", "heix", "hevc", "hevx", "heim", "heis", "hevm", "hevs":
			return true
		}
	}

	return false
}

func IsWebP(data []byte) bool {
	return len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP"
}
