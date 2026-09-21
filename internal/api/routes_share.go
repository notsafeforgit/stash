package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"html"
	"io"
	"io/fs"
	"math"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/sharing"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/ui"
)

var shareIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)
var shareAssetPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+\.(js|css|woff2|png|svg)$`)

type shareContextKey int

const shareGrantKey shareContextKey = 0

type shareRoutes struct {
	server  *Server
	service *sharing.Service
	budget  *shareBudget
}

// The public surface has its own router and middleware. It never goes through
// owner authentication, general GraphQL, plugins, custom files or SPA fallback.
func (s *Server) withShareRoutes(private http.Handler) http.Handler {
	budget := newShareBudget()
	budget.stopStream = func(fileID models.FileID, session string) {
		if s.manager.StreamManager != nil {
			s.manager.StreamManager.StopV3StreamsForSession(fileID, session, "", false)
		}
	}
	s.shares.OnInvalidate = budget.releaseShare
	public := (&shareRoutes{server: s, service: s.shares, budget: budget}).router()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/share" || strings.HasPrefix(r.URL.Path, "/share/") {
			public.ServeHTTP(w, r)
			return
		}
		private.ServeHTTP(w, r)
	})
}

func (rs *shareRoutes) router() http.Handler {
	r := chi.NewRouter()
	r.Use(shareHeaders)
	r.Get("/share", rs.page)
	r.Get("/share/", rs.page)
	r.Get("/share/assets/{asset}", rs.asset)
	r.Get("/share/{shareID}", rs.page)
	r.Route("/share/{shareID}", func(r chi.Router) {
		r.Get("/", rs.page)
		r.Post("/exchange", rs.exchange)
		r.Group(func(r chi.Router) {
			r.Use(rs.authorize)
			r.Get("/content", rs.content)
			r.Get("/status", func(w http.ResponseWriter, r *http.Request) {
				row := shareGrant(r)
				shareJSON(w, map[string]string{"expires_at": time.Unix(row.ExpiresAt, 0).UTC().Format(time.RFC3339), "server_time": time.Now().UTC().Format(time.RFC3339)})
			})
			r.Route("/media/{mediaKey}", func(r chi.Router) {
				r.Get("/", rs.detail)
				r.Get("/thumbnail", rs.rendition)
				r.Head("/thumbnail", rs.rendition)
				r.Get("/image", rs.rendition)
				r.Head("/image", rs.rendition)
				r.Get("/download", rs.download)
				r.Head("/download", rs.download)
				scene := sceneRoutes{routes: routes{txnManager: rs.service.Repo.TxnManager}, sceneFinder: rs.service.Repo.Scene, fileGetter: rs.service.Repo.File}
				for endpoint, handler := range map[string]http.HandlerFunc{
					"/stream.master.m3u8":                         scene.StreamV3HLSMaster,
					"/stream.m3u8/{track}.m3u8":                   scene.StreamV3HLSTrackPlaylist,
					"/stream.m3u8/{track}/init.mp4":               scene.StreamV3HLSInitSegment,
					"/stream.m3u8/{track}/{segment}.m4s":          scene.StreamV3HLSSegment,
					"/stream.fmp4.master.m3u8":                    scene.StreamV3HLSCopyFMP4Master,
					"/stream.fmp4.m3u8/{track}.m3u8":              scene.StreamV3HLSCopyFMP4TrackPlaylist,
					"/stream.fmp4.m3u8/{track}/init.mp4":          scene.StreamV3HLSCopyFMP4InitSegment,
					"/stream.fmp4.m3u8/{track}/{segment}.m4s":     scene.StreamV3HLSCopyFMP4Segment,
					"/stream.fmp4.aac.master.m3u8":                scene.StreamV3HLSCopyFMP4AACMaster,
					"/stream.fmp4.aac.m3u8/{track}.m3u8":          scene.StreamV3HLSCopyFMP4AACTrackPlaylist,
					"/stream.fmp4.aac.m3u8/{track}/init.mp4":      scene.StreamV3HLSCopyFMP4AACInitSegment,
					"/stream.fmp4.aac.m3u8/{track}/{segment}.m4s": scene.StreamV3HLSCopyFMP4AACSegment,
				} {
					r.Get(endpoint, rs.stream(handler))
					r.Head(endpoint, rs.stream(handler))
				}
				r.Post("/streams.stop", rs.stream(scene.StreamsStop))
				r.Post("/streams.keepalive", rs.stream(scene.StreamsKeepalive))
			})
		})
	})
	return r
}

func shareHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Stash-Share-Version", "1")
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' blob: data:; media-src 'self' blob:; connect-src 'self'; worker-src 'self' blob:; font-src 'self'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		// Avoid path normalization disagreements with the proxy or media router.
		if r.URL.RawPath != "" || strings.Contains(r.URL.Path, "\\") || strings.Contains(r.URL.Path, "//") || path.Clean(r.URL.Path) != strings.TrimSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sharePrefix(r *http.Request) string {
	prefix := getProxyPrefix(r)
	if prefix != "" && (path.Clean(prefix) != prefix || !strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, "?#\\\r\n") || strings.Contains(prefix, "//")) {
		return ""
	}
	return prefix + "/share/"
}

func shareBase(r *http.Request, id string) string { return sharePrefix(r) + id + "/" }
func shareCookieName(id string) string            { return "__Secure-stash_share_" + id }
func shareGrant(r *http.Request) *models.ShareRecord {
	return r.Context().Value(shareGrantKey).(*models.ShareRecord)
}

func sameShareOrigin(r *http.Request, require bool) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		return err == nil && u.Host == r.Host && u.User == nil && (u.Scheme == "https" || (u.Scheme == "http" && (strings.HasPrefix(r.Host, "localhost:") || strings.HasPrefix(r.Host, "127.0.0.1:"))))
	}
	return !require || r.Header.Get("Sec-Fetch-Site") == "same-origin"
}

func (rs *shareRoutes) page(w http.ResponseWriter, r *http.Request) {
	if id := chi.URLParam(r, "shareID"); id != "" && !shareIDPattern.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(ui.Box(true), "share.html")
	if err != nil {
		http.Error(w, "Share viewer unavailable", http.StatusServiceUnavailable)
		return
	}
	data = bytes.Replace(data, []byte(`<base href="/share/"`), []byte(`<base href="`+html.EscapeString(sharePrefix(r))+`"`), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (rs *shareRoutes) asset(w http.ResponseWriter, r *http.Request) {
	asset := chi.URLParam(r, "asset")
	if !shareAssetPattern.MatchString(asset) {
		http.NotFound(w, r)
		return
	}
	// Read an explicit bundled file, never a directory or the general app shell.
	data, err := fs.ReadFile(ui.Box(true), "assets/"+asset)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, asset, time.Time{}, bytes.NewReader(data))
}

func (rs *shareRoutes) exchange(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "shareID")
	if !shareIDPattern.MatchString(id) || !sameShareOrigin(r, true) || r.Header.Get("Content-Type") != "application/json" {
		http.NotFound(w, r)
		return
	}
	if !rs.budget.exchange(r.RemoteAddr) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Try again later", http.StatusTooManyRequests)
		return
	}
	var input struct {
		Secret string `json:"secret"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.NotFound(w, r)
		return
	}
	secret, row, err := rs.service.Exchange(r.Context(), id, input.Secret)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: shareCookieName(id), Value: secret, Path: shareBase(r, id), Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: time.Unix(row.ExpiresAt, 0), MaxAge: int(row.ExpiresAt - time.Now().Unix())})
	w.WriteHeader(http.StatusNoContent)
}

func (rs *shareRoutes) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "shareID")
		if !shareIDPattern.MatchString(id) || !sameShareOrigin(r, false) {
			http.NotFound(w, r)
			return
		}
		cookie, err := r.Cookie(shareCookieName(id))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		row, err := rs.service.Authenticate(r.Context(), id, cookie.Value)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ctx, done, err := rs.service.BeginDelivery(r.Context(), row)
		if err != nil {
			w.Header().Set("Retry-After", "2")
			http.Error(w, "Share is busy", http.StatusTooManyRequests)
			return
		}
		defer done()
		// A slow client can block inside Write; cancellation must interrupt that
		// write too, rather than waiting for the next buffer boundary.
		stopDeadline := context.AfterFunc(ctx, func() { _ = http.NewResponseController(w).SetWriteDeadline(time.Now()) })
		defer stopDeadline()
		// Revalidate after registering the cancellable delivery.
		row, err = rs.service.Authenticate(ctx, id, cookie.Value)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ctx = context.WithValue(ctx, shareGrantKey, row)
		r = r.WithContext(ctx)
		next.ServeHTTP(&shareResponse{ResponseWriter: w, ctx: ctx, budget: rs.budget, shareID: id}, r)
	})
}

type publicShareEntry struct {
	Kind      string   `json:"kind"`
	Title     string   `json:"title"`
	MediaKeys []string `json:"media_keys"`
}
type publicShareMedia struct {
	Key       string  `json:"key"`
	Kind      string  `json:"kind"`
	Title     string  `json:"title"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Duration  float64 `json:"duration"`
	Video     bool    `json:"video"`
	Thumbnail string  `json:"thumbnail"`
	Image     string  `json:"image"`
	Download  string  `json:"download"`
}
type publicShareContent struct {
	Label      string             `json:"label"`
	ExpiresAt  string             `json:"expires_at"`
	ServerTime string             `json:"server_time"`
	Entries    []publicShareEntry `json:"entries"`
	Media      []publicShareMedia `json:"media"`
}
type publicShareDetail struct {
	Media      publicShareMedia               `json:"media"`
	VideoCodec string                         `json:"video_codec"`
	AudioCodec string                         `json:"audio_codec"`
	FrameRate  float64                        `json:"frame_rate"`
	Streams    []*manager.SceneStreamEndpoint `json:"streams"`
}

func publicMedia(r *http.Request, row *models.ShareRecord, item models.ShareMedia) publicShareMedia {
	base := shareBase(r, row.ID) + "media/" + item.Key + "/"
	ret := publicShareMedia{Key: item.Key, Kind: item.Kind, Width: item.Width, Height: item.Height, Duration: item.Duration, Video: item.Duration > 0, Thumbnail: base + "thumbnail", Image: base + "image"}
	if row.ShowMetadata {
		ret.Title = item.Title
	}
	if row.AllowDownload {
		ret.Download = base + "download"
	}
	return ret
}

func (rs *shareRoutes) content(w http.ResponseWriter, r *http.Request) {
	row := shareGrant(r)
	snapshot, err := sharing.Snapshot(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ret := publicShareContent{Label: row.Label, ExpiresAt: time.Unix(row.ExpiresAt, 0).UTC().Format(time.RFC3339), ServerTime: time.Now().UTC().Format(time.RFC3339), Entries: []publicShareEntry{}, Media: []publicShareMedia{}}
	for _, entry := range snapshot.Entries {
		title := ""
		if row.ShowMetadata {
			title = entry.Title
		}
		ret.Entries = append(ret.Entries, publicShareEntry{Kind: entry.Kind, Title: title, MediaKeys: entry.MediaKeys})
	}
	for _, media := range snapshot.Media {
		ret.Media = append(ret.Media, publicMedia(r, row, media))
	}
	shareJSON(w, ret)
}

func (rs *shareRoutes) item(r *http.Request) (*models.ShareMedia, models.File, *models.Scene, error) {
	snapshot, err := sharing.Snapshot(shareGrant(r))
	if err != nil {
		return nil, nil, nil, err
	}
	key := chi.URLParam(r, "mediaKey")
	for _, item := range snapshot.Media {
		if item.Key == key {
			f, scene, err := rs.service.Resolve(r.Context(), item)
			if scene == nil && err == nil {
				if vf, ok := f.(*models.VideoFile); ok {
					scene = &models.Scene{ID: item.EntityID, Files: models.NewRelatedVideoFiles([]*models.VideoFile{vf})}
				}
			}
			return &item, f, scene, err
		}
	}
	return nil, nil, nil, sharing.ErrUnavailable
}

func (rs *shareRoutes) detail(w http.ResponseWriter, r *http.Request) {
	row := shareGrant(r)
	item, _, scene, err := rs.item(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ret := publicShareDetail{Media: publicMedia(r, row, *item), VideoCodec: item.VideoCodec, AudioCodec: item.AudioCodec, FrameRate: item.FrameRate, Streams: []*manager.SceneStreamEndpoint{}}
	if scene != nil {
		base := &url.URL{Path: shareBase(r, row.ID) + "media/" + item.Key + "/stream"}
		streams, err := manager.GetV3SceneStreamPaths(scene, base, models.StreamingResolutionEnumFullHd)
		if err != nil {
			http.Error(w, "Media unavailable", http.StatusServiceUnavailable)
			return
		}
		for _, stream := range streams {
			u, err := url.Parse(stream.URL)
			if err == nil && strings.HasSuffix(u.Path, ".master.m3u8") {
				ret.Streams = append(ret.Streams, stream)
			}
		}
	}
	shareJSON(w, ret)
}

func shareJSON(w http.ResponseWriter, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "Media unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (rs *shareRoutes) stream(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, _, scene, err := rs.item(r)
		if err != nil || scene == nil {
			http.NotFound(w, r)
			return
		}
		row := shareGrant(r)
		// Separate encoder sessions from the owner's sessions and from every
		// other grant. Ignore client-supplied session identifiers entirely.
		cookie, _ := r.Cookie(shareCookieName(row.ID))
		clientSession, err := ffmpeg.ParseV3StreamSession(r.URL.Query().Get("stream_session"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		sessionID := "share-" + hex.EncodeToString(sharing.Hash(cookie.Value + "/" + item.Key + "/" + clientSession))[:32]
		q := r.URL.Query()
		clean := url.Values{"stream_session": {sessionID}}
		for _, key := range []string{"start", "end"} {
			if value := q.Get(key); value != "" {
				n, err := strconv.ParseFloat(value, 64)
				if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > item.Duration {
					http.NotFound(w, r)
					return
				}
				clean.Set(key, value)
			}
		}
		for _, key := range []string{"resolution", "keep_resolution"} {
			value := q.Get(key)
			if value == "" || value == string(models.StreamingResolutionEnumOriginal) {
				value = string(models.StreamingResolutionEnumFullHd)
			}
			switch models.StreamingResolutionEnum(value) {
			case models.StreamingResolutionEnumFullHd, models.StreamingResolutionEnumStandardHd, models.StreamingResolutionEnumStandard, models.StreamingResolutionEnumLow:
				clean.Set(key, value)
			default:
				http.NotFound(w, r)
				return
			}
		}
		if value := q.Get("keep_type"); value != "" {
			if value != "hls" && value != "hls-copy-fmp4" && value != "hls-copy-fmp4-aac" {
				http.NotFound(w, r)
				return
			}
			clean.Set("keep_type", value)
		}
		if q.Get("release") == "1" {
			clean.Set("release", "1")
		}
		// Count every encoder variant, not just browser sessions. A recipient
		// cannot multiply the budget by requesting many resolutions/codecs.
		streamType := "hls"
		if strings.Contains(r.URL.Path, "/stream.fmp4.aac.") {
			streamType = "hls-copy-fmp4-aac"
		} else if strings.Contains(r.URL.Path, "/stream.fmp4.") {
			streamType = "hls-copy-fmp4"
		}
		resolution := clean.Get("resolution")
		if strings.HasSuffix(r.URL.Path, "/streams.keepalive") {
			streamType = clean.Get("keep_type")
			resolution = clean.Get("keep_resolution")
		}
		if strings.HasSuffix(r.URL.Path, "/streams.stop") {
			if clean.Get("keep_type") == "" {
				rs.budget.releaseSession(sessionID)
			} else {
				rs.budget.forgetOtherVariants(sessionID, sessionID+"/"+clean.Get("keep_type")+"/"+clean.Get("keep_resolution"))
			}
		} else if !rs.budget.stream(row.ID, sessionID+"/"+streamType+"/"+resolution, item.FileID, sessionID, time.Unix(row.ExpiresAt, 0)) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Share playback limit reached", http.StatusTooManyRequests)
			return
		}
		r.URL.RawQuery = clean.Encode()
		r = r.WithContext(ffmpeg.WithPrivateV3Stream(context.WithValue(r.Context(), sceneKey, scene)))
		stopCancellation := context.AfterFunc(r.Context(), func() { rs.budget.releaseSession(sessionID) })
		defer stopCancellation()
		if r.Context().Err() != nil {
			return
		}
		handler(w, r)
	}
}

func (rs *shareRoutes) download(w http.ResponseWriter, r *http.Request) {
	if !shareGrant(r).AllowDownload {
		http.NotFound(w, r)
		return
	}
	item, f, _, err := rs.item(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ext := strings.ToLower(path.Ext(f.Base().Basename))
	if len(ext) > 12 || strings.ContainsAny(ext, "\"\\/\r\n") {
		ext = ""
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+item.Key+ext+`"`)
	if err := f.Base().Serve(&file.OsFS{}, w, r); err != nil {
		http.NotFound(w, r)
	}
}
