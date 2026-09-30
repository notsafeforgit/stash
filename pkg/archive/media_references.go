package archive

import (
	"encoding/json"
	"math/big"
	"net/url"
	"strconv"
	"strings"
)

func sourceURL(value interface{}) *url.URL {
	text, ok := value.(string)
	if !ok || (!strings.HasPrefix(text, "https://") && !strings.HasPrefix(text, "http://")) {
		return nil
	}
	u, err := url.Parse(text)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	u.Host = strings.ToLower(u.Host)
	return u
}

func usableRendition(value interface{}) bool {
	item, ok := value.(sourceObject)
	if !ok {
		return false
	}
	for _, key := range []string{"u", "url", "gif", "mp4"} {
		if sourceURL(item[key]) != nil {
			return true
		}
	}
	return false
}

func renditionDimension(value interface{}) *big.Int {
	ret := new(big.Int)
	var number string
	switch value := value.(type) {
	case json.Number:
		number = value.String()
		if strings.ContainsAny(number, ".eE") {
			f, err := strconv.ParseFloat(number, 64)
			if err != nil || f <= 0 {
				return ret
			}
			ret, _ = new(big.Float).SetFloat64(f).Int(ret)
			return ret
		}
	case string:
		number = strings.TrimSpace(value)
	case bool:
		if value {
			return big.NewInt(1)
		}
	}
	// This is a rendition-ranking bound, not source-data truncation. Dimensions
	// of more than 128 digits are unusable; their original JSON stays retained.
	if len(number) > 128 {
		return ret
	}
	if _, ok := ret.SetString(number, 10); !ok || ret.Sign() < 0 {
		return new(big.Int)
	}
	return ret
}

func renditionSize(item sourceObject) [3]*big.Int {
	x, hasX := item["x"]
	if !hasX {
		x = item["width"]
	}
	y, hasY := item["y"]
	if !hasY {
		y = item["height"]
	}
	w, h := renditionDimension(x), renditionDimension(y)
	return [3]*big.Int{new(big.Int).Mul(w, h), w, h}
}

func bestRendition(value interface{}) sourceObject {
	items, _ := value.([]interface{})
	var best sourceObject
	var largest [3]*big.Int
	for _, candidate := range items {
		if !usableRendition(candidate) {
			continue
		}
		item := candidate.(sourceObject)
		size := renditionSize(item)
		greater := best == nil
		if best != nil {
			for i, dimension := range size {
				if c := dimension.Cmp(largest[i]); c != 0 {
					greater = c > 0
					break
				}
			}
		}
		if greater {
			best, largest = item, size
		}
	}
	return best
}

func retainGalleryRenditions(item sourceObject) {
	if usableRendition(item["s"]) {
		delete(item, "p")
		delete(item, "o")
	} else if best := bestRendition(item["p"]); best != nil {
		item["p"] = []interface{}{best}
		delete(item, "o")
	} else {
		delete(item, "p")
		if best := bestRendition(item["o"]); best != nil {
			item["o"] = []interface{}{best}
		} else {
			delete(item, "o")
		}
	}
}

func retainPreviewRenditions(item sourceObject) {
	if usableRendition(item["source"]) {
		delete(item, "resolutions")
	} else if best := bestRendition(item["resolutions"]); best != nil {
		item["resolutions"] = []interface{}{best}
	} else {
		delete(item, "resolutions")
	}
	variants, ok := item["variants"].(sourceObject)
	if !ok {
		return
	}
	unblurred := usableRendition(item["source"]) || bestRendition(item["resolutions"]) != nil
	for _, key := range []string{"gif", "mp4"} {
		if variant, ok := variants[key].(sourceObject); ok && usableRendition(variant["source"]) {
			unblurred = true
		}
	}
	for kind, value := range variants {
		if variant, ok := value.(sourceObject); ok {
			retainPreviewRenditions(variant)
			if unblurred && (kind == "obfuscated" || kind == "nsfw" || kind == "nsfw_blurred" || kind == "blurred") {
				delete(variants, kind)
			}
		}
	}
	if len(variants) == 0 {
		delete(item, "variants")
	}
}

func redditImageID(value interface{}) string {
	u := sourceURL(value)
	if u == nil || (u.Hostname() != "i.redd.it" && u.Hostname() != "preview.redd.it") {
		return ""
	}
	file := u.EscapedPath()
	file = file[strings.LastIndex(file, "/")+1:]
	if pos := strings.LastIndex(file, "."); pos >= 0 {
		file = file[:pos]
	}
	return file
}

func retainRedditRenditions(data sourceObject) {
	originals, sources := make(map[string]bool), make(map[string]bool)
	// A per-file _url must not alter the common post body.
	for _, key := range []string{"url", "url_overridden_by_dest"} {
		if u := sourceURL(data[key]); u != nil && u.Hostname() == "i.redd.it" {
			if id := redditImageID(data[key]); id != "" {
				originals[id] = true
			}
		}
	}
	gallery, _ := data["media_metadata"].(sourceObject)
	for id, value := range gallery {
		if item, ok := value.(sourceObject); ok {
			retainGalleryRenditions(item)
			if id != "" && usableRendition(item["s"]) {
				sources[id] = true
			}
		}
	}
	video := false
	for _, key := range []string{"media", "secure_media"} {
		if media, ok := data[key].(sourceObject); ok {
			if stream, ok := media["reddit_video"].(sourceObject); ok {
				for _, key := range []string{"fallback_url", "dash_url", "hls_url"} {
					video = video || sourceURL(stream[key]) != nil
				}
			}
		}
	}
	if preview, ok := data["preview"].(sourceObject); ok {
		if images, ok := preview["images"].([]interface{}); ok {
			kept := make([]interface{}, 0, len(images))
			for _, value := range images {
				item, ok := value.(sourceObject)
				if !ok {
					kept = append(kept, value)
					continue
				}
				retainPreviewRenditions(item)
				source, _ := item["source"].(sourceObject)
				if !sourceTruthy(item["source"]) {
					source = bestRendition(item["resolutions"])
				}
				mid := redditImageID(source["url"])
				variants, _ := item["variants"].(sourceObject)
				_, gif := variants["gif"]
				_, mp4 := variants["mp4"]
				id, _ := item["id"].(string)
				if video || (!gif && !mp4 && (originals[mid] || sources[id] || sources[mid])) {
					continue
				}
				kept = append(kept, item)
			}
			if len(kept) == 0 {
				delete(preview, "images")
			} else {
				preview["images"] = kept
			}
		}
		if video {
			delete(preview, "reddit_video_preview")
		}
		if len(preview) == 0 || (len(preview) == 1 && hasSourceKey(preview, "enabled")) {
			delete(data, "preview")
		}
	}
	hasMedia := len(originals) > 0 || len(sources) > 0 || video
	for _, value := range gallery {
		if item, ok := value.(sourceObject); ok {
			hasMedia = hasMedia || bestRendition(item["p"]) != nil || bestRendition(item["o"]) != nil
		}
	}
	if preview, ok := data["preview"].(sourceObject); ok {
		if images, ok := preview["images"].([]interface{}); ok {
			for _, value := range images {
				if item, ok := value.(sourceObject); ok {
					hasMedia = hasMedia || usableRendition(item["source"]) || bestRendition(item["resolutions"]) != nil
				}
			}
		}
		_, hasVideoPreview := preview["reddit_video_preview"].(sourceObject)
		hasMedia = hasMedia || hasVideoPreview
	}
	if hasMedia {
		for _, key := range []string{"thumbnail", "thumbnail_width", "thumbnail_height"} {
			delete(data, key)
		}
	}
}

func hasSourceKey(value sourceObject, key string) bool {
	_, ok := value[key]
	return ok
}

func retainMedia(value interface{}, reddit bool) interface{} {
	switch value := value.(type) {
	case []interface{}:
		ret := make([]interface{}, len(value))
		for i, child := range value {
			ret[i] = retainMedia(child, reddit)
		}
		return ret
	case sourceObject:
		reddit = reddit || value["category"] == "reddit"
		ret := make(sourceObject)
		for key, child := range value {
			childReddit := key == "_reddit" || (reddit && (key == "crosspost_parent_list" || key == "comment" || key == "enrichment_attachments"))
			ret[key] = retainMedia(child, childReddit)
		}
		if reddit {
			retainRedditRenditions(ret)
		}
		return ret
	default:
		return value
	}
}
