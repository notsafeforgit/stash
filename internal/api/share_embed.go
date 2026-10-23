package api

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/internal/sharing"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/ffmpeg/transcoder"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/utils"
)

// Link previews are authenticated with the URL capability, never a bot's
// User-Agent. Crawlers cannot exchange fragment secrets or retain guest cookies.
// A share containing one distinct media file is also a single-item share when
// it was created by selecting a gallery or overlapping library entities.
func (rs *shareRoutes) linkItem(r *http.Request) (*models.ShareMedia, models.File, *models.Scene, error) {
	source, err := sharing.Source(shareGrant(r))
	if err != nil || (source != nil && source.Mode == "ONGOING") {
		return nil, nil, nil, sharing.ErrUnavailable
	}
	snapshot, err := sharing.Snapshot(shareGrant(r))
	if err != nil || len(snapshot.Media) != 1 {
		return nil, nil, nil, sharing.ErrUnavailable
	}
	return rs.service.Item(r.Context(), shareGrant(r), snapshot.Media[0].Key)
}

func shareLinkBase(r *http.Request, id string) string {
	base := config.GetInstance().GetSharingPublicURL()
	if base == "" {
		scheme := "http"
		if r.TLS != nil || r.URL.Scheme == "https" || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		base = scheme + "://" + r.Host + strings.TrimSuffix(sharePrefix(r), "/")
	}
	return strings.TrimRight(base, "/") + "/" + id + "/"
}

func (rs *shareRoutes) linkPage(w http.ResponseWriter, r *http.Request) {
	row := shareGrant(r)
	snapshot, err := sharing.Snapshot(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	base := shareLinkBase(r, row.ID)
	query := "?" + url.Values{"key": {r.URL.Query().Get("key")}}.Encode()
	title := row.Label
	card, kind := "summary", "website"
	var tags strings.Builder
	meta := func(property, value string) {
		fmt.Fprintf(&tags, `<meta property="%s" content="%s"/>`+"\n", property, html.EscapeString(value))
	}
	if len(snapshot.Media) == 1 {
		item, f, _, err := rs.linkItem(r)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if row.ShowMetadata && strings.TrimSpace(item.Title) != "" {
			title = item.Title
		}
		card = "summary_large_image"
		meta("og:image", base+"preview/image"+query)
		if vf, ok := f.(*models.VideoFile); ok {
			card, kind = "player", "video.other"
			video := base + "preview/video.mp4" + query
			width, height := item.Width, item.Height
			if !directLinkVideo(vf) && min(width, height) > 720 {
				scale := 720.0 / float64(min(width, height))
				width, height = int(float64(width)*scale)&^1, int(float64(height)*scale)&^1
			}
			meta("og:video", video)
			meta("og:video:url", video)
			meta("og:video:secure_url", video)
			meta("og:video:type", "video/mp4")
			meta("og:video:width", strconv.Itoa(width))
			meta("og:video:height", strconv.Itoa(height))
			meta("video:duration", strconv.Itoa(int(item.Duration)))
			meta("twitter:player:stream", video)
			meta("twitter:player:stream:content_type", "video/mp4")
			meta("twitter:player:width", strconv.Itoa(width))
			meta("twitter:player:height", strconv.Itoa(height))
			// FxEmbed uses the OG poster while suppressing the Twitter image
			// fallback, so preview consumers select the video stream card.
			meta("twitter:image", "0")
		} else {
			meta("twitter:image", base+"preview/image"+query)
		}
	}
	meta("og:type", kind)
	meta("og:site_name", "Stash")
	meta("og:url", base+query)
	meta("og:title", title)
	meta("twitter:title", title)
	meta("twitter:card", card)
	rs.servePage(w, r, tags.String())
}

func linkMediaHeaders(w http.ResponseWriter) {
	// These endpoints use an explicit bearer capability, never ambient cookies.
	// Telegram/web clients may render the authorized bytes on another origin.
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

func (rs *shareRoutes) linkImage(w http.ResponseWriter, r *http.Request) {
	item, f, scene, err := rs.linkItem(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	linkMediaHeaders(w)
	if _, ok := f.(*models.ImageFile); ok {
		switch sharedOriginalMIME(f) {
		case "image/jpeg", "image/png", "image/gif", "image/webp":
			rs.serveOriginal(w, r, item, f, false)
			return
		}
	}
	if rs.existingRendition(w, r, item, f, scene, false) {
		return
	}
	// AVIF/HEIC and video-backed images need a broadly supported still. Create
	// only a bounded single JPEG, with no persistent share-specific artwork.
	if f.Base().ZipFileID != nil || rs.server.manager.FFMpeg == nil {
		http.NotFound(w, r)
		return
	}
	if !rs.beginLinkEncode(w) {
		return
	}
	defer func() { <-rs.budget.linkEncodes }()
	args := transcoder.ImageThumbnail(f.Base().Path, transcoder.ImageThumbnailOptions{MaxDimensions: 1280, Quality: 3})
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	data, err := rs.server.manager.FFMpeg.GenerateOutput(ctx, args, nil)
	if err != nil {
		http.Error(w, "Preview unavailable", http.StatusServiceUnavailable)
		return
	}
	utils.ServeImage(w, r, data)
}

// Do not advertise an AV1/HEVC/MKV original as an MP4 that Telegram can decode.
// Existing AVC/AAC MP4s need no encoder and retain length and byte-range support.
func directLinkVideo(vf *models.VideoFile) bool {
	if vf.BitDepth != nil && *vf.BitDepth > 8 {
		return false
	}
	if vf.ColorTransfer != nil {
		switch strings.ToLower(*vf.ColorTransfer) {
		case "arib-std-b67", "smpte2084":
			return false
		}
	}
	return sharedOriginalMIME(vf) == "video/mp4" && vf.VideoCodec == ffmpeg.H264 &&
		(vf.AudioCodec == "" || strings.EqualFold(vf.AudioCodec, "aac"))
}

func (rs *shareRoutes) beginLinkEncode(w http.ResponseWriter) bool {
	select {
	case rs.budget.linkEncodes <- struct{}{}:
		return true
	default:
		w.Header().Set("Retry-After", "5")
		http.Error(w, "Preview busy", http.StatusTooManyRequests)
		return false
	}
}

func (rs *shareRoutes) linkVideo(w http.ResponseWriter, r *http.Request) {
	item, f, _, err := rs.linkItem(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	vf, ok := f.(*models.VideoFile)
	if !ok {
		http.NotFound(w, r)
		return
	}
	linkMediaHeaders(w)
	if directLinkVideo(vf) {
		rs.serveOriginal(w, r, item, f, false)
		return
	}
	if vf.ZipFileID != nil || rs.server.manager.StreamManager == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodHead {
		if !rs.beginLinkEncode(w) {
			return
		}
		defer func() { <-rs.budget.linkEncodes }()
	}
	// Reuse the cancellation-aware compatibility stream. No complete video is
	// buffered or stored; revoked shares and disconnected crawlers stop encoding.
	rs.server.manager.StreamManager.ServeDownload(w, r, ffmpeg.DownloadOptions{
		VideoFile: vf, Mode: ffmpeg.DownloadModeH264, Resolution: models.StreamingResolutionEnumStandardHd,
		Filename: "shared-video", Inline: true,
	})
}
