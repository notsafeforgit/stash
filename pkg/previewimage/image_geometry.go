package previewimage

import (
	"fmt"
	"strings"
)

// Apply display properties before resizing or creating a gain map. avifdec
// 1.4+ bakes crop/rotation/mirror into PNG; it still leaves pixel aspect ratio.
func avifTransformFilters(info string, nativeTransforms bool) ([]string, error) {
	var filters []string
	var clap, cropped bool
	var aspectW, aspectH, angle int
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		label, value, _ := strings.Cut(line, ":")
		switch strings.TrimSpace(label) {
		case "pasp (Aspect Ratio)":
			if _, err := fmt.Sscanf(value, "%d/%d", &aspectW, &aspectH); err != nil || aspectW <= 0 || aspectH <= 0 {
				return nil, fmt.Errorf("invalid AVIF pixel aspect ratio")
			}
			filters = append(filters, fmt.Sprintf("scale=w='max(1,round(iw*%d/%d))':h=ih:flags=lanczos", aspectW, aspectH), "setsar=1")
		case "clap (Clean Aperture)":
			clap = true
		case "Valid, derived crop rect":
			var x, y, w, h int
			if _, err := fmt.Sscanf(value, " X: %d, Y: %d, W: %d, H: %d", &x, &y, &w, &h); err != nil || x < 0 || y < 0 || w <= 0 || h <= 0 {
				return nil, fmt.Errorf("invalid AVIF crop")
			}
			// Crop uses source pixel coordinates, before aspect correction.
			filters = append([]string{fmt.Sprintf("crop=%d:%d:%d:%d", w, h, x, y)}, filters...)
			cropped = true
		case "irot (Rotation)":
			if _, err := fmt.Sscanf(value, "%d", &angle); err != nil || angle < 0 || angle > 3 {
				return nil, fmt.Errorf("invalid AVIF rotation")
			}
			switch angle {
			case 1:
				filters = append(filters, "transpose=cclock")
			case 2:
				filters = append(filters, "hflip", "vflip")
			case 3:
				filters = append(filters, "transpose=clock")
			}
		case "imir (Mirror)":
			var axis int
			if _, err := fmt.Sscanf(value, "%d", &axis); err != nil || axis < 0 || axis > 1 {
				return nil, fmt.Errorf("invalid AVIF mirror")
			}
			filters = append(filters, []string{"vflip", "hflip"}[axis])
		}
	}
	if clap && !cropped {
		return nil, fmt.Errorf("invalid AVIF clean aperture")
	}
	if nativeTransforms {
		if aspectW == 0 {
			return nil, nil
		}
		if angle%2 != 0 {
			return []string{fmt.Sprintf("scale=w=iw:h='max(1,round(ih*%d/%d))':flags=lanczos", aspectW, aspectH), "setsar=1"}, nil
		}
		return []string{fmt.Sprintf("scale=w='max(1,round(iw*%d/%d))':h=ih:flags=lanczos", aspectW, aspectH), "setsar=1"}, nil
	}
	return filters, nil
}
