package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/internal/static"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/file/video"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/utils"
)

type SceneFinder interface {
	models.SceneGetter

	FindByChecksum(ctx context.Context, checksum string) ([]*models.Scene, error)
	FindByOSHash(ctx context.Context, oshash string) ([]*models.Scene, error)
	GetCover(ctx context.Context, sceneID int) ([]byte, error)
}

type SceneMarkerFinder interface {
	models.SceneMarkerGetter
	FindBySceneID(ctx context.Context, sceneID int) ([]*models.SceneMarker, error)
}

type SceneMarkerTagFinder interface {
	models.TagGetter
	FindBySceneMarkerID(ctx context.Context, sceneMarkerID int) ([]*models.Tag, error)
}

type CaptionFinder interface {
	GetCaptions(ctx context.Context, fileID models.FileID) ([]*models.VideoCaption, error)
}

type sceneRoutes struct {
	routes
	sceneFinder       SceneFinder
	fileGetter        models.FileGetter
	captionFinder     CaptionFinder
	sceneMarkerFinder SceneMarkerFinder
	tagFinder         SceneMarkerTagFinder
}

func (rs sceneRoutes) Routes() chi.Router {
	r := chi.NewRouter()

	r.Route("/{sceneId}", func(r chi.Router) {
		r.Use(rs.SceneCtx)

		// streaming endpoints
		r.Get("/stream", rs.StreamDirect)
		r.Get("/stream.mp4", rs.StreamMp4)
		r.Get("/stream.webm", rs.StreamWebM)
		r.Get("/stream.mkv", rs.StreamMKV)
		r.Get("/stream.m3u8", rs.StreamHLS)
		r.Get("/stream.m3u8/{segment}.ts", rs.StreamHLSSegment)
		r.Get("/stream.mpd", rs.StreamDASH)
		r.Get("/stream.mpd/{segment}_v.webm", rs.StreamDASHVideoSegment)
		r.Get("/stream.mpd/{segment}_a.webm", rs.StreamDASHAudioSegment)

		if config.GetInstance().GetEnableV3UI() {
			// HLS transcode (re-encode to H.264 + AAC fMP4). The
			// `.master.m3u8` endpoint serves a multivariant master playlist with
			// EXT-X-MEDIA audio rendition + EXT-X-STREAM-INF video variant, both
			// pointing at per-track media playlists under `/stream.m3u8/{track}.m3u8`.
			r.Get("/stream.master.m3u8", rs.StreamV3HLSMaster)
			r.Get("/stream.m3u8/{track}.m3u8", rs.StreamV3HLSTrackPlaylist)
			r.Get("/stream.m3u8/{track}/init.mp4", rs.StreamV3HLSInitSegment)
			r.Get("/stream.m3u8/{track}/{segment}.m4s", rs.StreamV3HLSSegment)

			// HLS codec-copy fMP4 (no re-encode, source codecs only).
			r.Get("/stream.fmp4.master.m3u8", rs.StreamV3HLSCopyFMP4Master)
			r.Get("/stream.fmp4.m3u8/{track}.m3u8", rs.StreamV3HLSCopyFMP4TrackPlaylist)
			r.Get("/stream.fmp4.m3u8/{track}/init.mp4", rs.StreamV3HLSCopyFMP4InitSegment)
			r.Get("/stream.fmp4.m3u8/{track}/{segment}.m4s", rs.StreamV3HLSCopyFMP4Segment)

			// HLS video-copy + AAC-transcode fMP4. Same fMP4 segment layout as
			// the codec-copy variant, but the audio bitstream is re-encoded
			// to AAC. Targets browsers that take the source video in fMP4 but
			// not the source audio (notably iOS Safari rejecting Opus-in-MP4
			// from ManagedMediaSource).
			r.Get("/stream.fmp4.aac.master.m3u8", rs.StreamV3HLSCopyFMP4AACMaster)
			r.Get("/stream.fmp4.aac.m3u8/{track}.m3u8", rs.StreamV3HLSCopyFMP4AACTrackPlaylist)
			r.Get("/stream.fmp4.aac.m3u8/{track}/init.mp4", rs.StreamV3HLSCopyFMP4AACInitSegment)
			r.Get("/stream.fmp4.aac.m3u8/{track}/{segment}.m4s", rs.StreamV3HLSCopyFMP4AACSegment)

			r.Post("/streams.stop", rs.StreamsStop)
			r.Post("/streams.keepalive", rs.StreamsKeepalive)

			r.Get("/download.mp4", rs.DownloadMP4)
			r.Head("/download.mp4", rs.DownloadMP4)
		}

		r.Get("/screenshot", rs.Screenshot)
		r.Get("/preview", rs.Preview)
		r.Get("/webp", rs.Webp)
		r.Get("/vtt/chapter", rs.VttChapter)
		r.Get("/vtt/thumbs", rs.VttThumbs)
		r.Get("/vtt/sprite", rs.VttSprite)
		r.Get("/funscript", rs.Funscript)
		r.Get("/interactive_csv", rs.InteractiveCSV)
		r.Get("/interactive_heatmap", rs.InteractiveHeatmap)
		r.Get("/caption", rs.CaptionLang)

		r.Get("/scene_marker/{sceneMarkerId}/stream", rs.SceneMarkerStream)
		r.Get("/scene_marker/{sceneMarkerId}/preview", rs.SceneMarkerPreview)
		r.Get("/scene_marker/{sceneMarkerId}/screenshot", rs.SceneMarkerScreenshot)
	})
	r.Get("/{sceneHash}_thumbs.vtt", rs.VttThumbs)
	r.Get("/{sceneHash}_sprite.jpg", rs.VttSprite)

	return r
}

func (rs sceneRoutes) StreamDirect(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	ss := manager.SceneServer{
		TxnManager:       rs.txnManager,
		SceneCoverGetter: rs.sceneFinder,
	}
	ss.StreamSceneDirect(scene, w, r)
}

func (rs sceneRoutes) StreamMp4(w http.ResponseWriter, r *http.Request) {
	rs.streamTranscode(w, r, ffmpeg.StreamTypeMP4)
}

func (rs sceneRoutes) StreamWebM(w http.ResponseWriter, r *http.Request) {
	rs.streamTranscode(w, r, ffmpeg.StreamTypeWEBM)
}

func (rs sceneRoutes) StreamMKV(w http.ResponseWriter, r *http.Request) {
	// only allow mkv streaming if the scene container is an mkv already
	scene := r.Context().Value(sceneKey).(*models.Scene)

	pf := scene.Files.Primary()
	if pf == nil {
		return
	}

	container, err := manager.GetVideoFileContainer(pf)
	if err != nil {
		logger.Errorf("[transcode] error getting container: %v", err)
	}

	if container != ffmpeg.Matroska {
		w.WriteHeader(http.StatusBadRequest)
		if _, err := w.Write([]byte("not an mkv file")); err != nil {
			logger.Warnf("[stream] error writing to stream: %v", err)
		}
		return
	}

	rs.streamTranscode(w, r, ffmpeg.StreamTypeMKV)
}

func (rs sceneRoutes) streamTranscode(w http.ResponseWriter, r *http.Request, streamType ffmpeg.StreamFormat) {
	scene := r.Context().Value(sceneKey).(*models.Scene)

	streamManager := manager.GetInstance().StreamManager
	if streamManager == nil {
		http.Error(w, "Live transcoding disabled", http.StatusServiceUnavailable)
		return
	}

	f := scene.Files.Primary()
	if f == nil {
		return
	}

	if err := r.ParseForm(); err != nil {
		logger.Warnf("[transcode] error parsing query form: %v", err)
	}

	startTime := r.Form.Get("start")
	ss, _ := strconv.ParseFloat(startTime, 64)
	resolution := r.Form.Get("resolution")

	options := ffmpeg.TranscodeOptions{
		StreamType: streamType,
		VideoFile:  f,
		Resolution: resolution,
		StartTime:  ss,
	}

	logger.Debugf("[transcode] streaming scene %d as %s", scene.ID, streamType.MimeType)
	streamManager.ServeTranscode(w, r, options)
}

func (rs sceneRoutes) StreamHLS(w http.ResponseWriter, r *http.Request) {
	rs.streamManifest(w, r, ffmpeg.StreamTypeHLS, "HLS")
}

func (rs sceneRoutes) StreamDASH(w http.ResponseWriter, r *http.Request) {
	rs.streamManifest(w, r, ffmpeg.StreamTypeDASHVideo, "DASH")
}

func (rs sceneRoutes) StreamV3HLSTrackPlaylist(w http.ResponseWriter, r *http.Request) {
	rs.streamV3Manifest(w, r, ffmpeg.V3StreamTypeHLS, "HLS track playlist")
}

func (rs sceneRoutes) StreamV3HLSMaster(w http.ResponseWriter, r *http.Request) {
	rs.streamV3MasterManifest(w, r, ffmpeg.V3StreamTypeHLS, "HLS master")
}

func (rs sceneRoutes) StreamV3HLSCopyFMP4Master(w http.ResponseWriter, r *http.Request) {
	rs.streamV3MasterManifest(w, r, ffmpeg.V3StreamTypeHLSCopyFMP4, "HLS (copy fMP4) master")
}

func (rs sceneRoutes) streamV3MasterManifest(w http.ResponseWriter, r *http.Request, streamType *ffmpeg.V3StreamType, logName string) {
	scene := r.Context().Value(sceneKey).(*models.Scene)

	streamManager := manager.GetInstance().StreamManager
	if streamManager == nil {
		http.Error(w, "Live transcoding disabled", http.StatusServiceUnavailable)
		return
	}

	f := scene.Files.Primary()
	if f == nil {
		return
	}

	if err := r.ParseForm(); err != nil {
		logger.Warnf("[transcode] error parsing query form: %v", err)
	}

	resolution := r.Form.Get("resolution")

	ffmpeg.TranscodeDebugf("[transcode] returning %s manifest for scene %d", logName, scene.ID)
	streamManager.ServeV3MasterManifest(w, r, streamType, f, resolution)
}

func (rs sceneRoutes) streamManifest(w http.ResponseWriter, r *http.Request, streamType *ffmpeg.StreamType, logName string) {
	scene := r.Context().Value(sceneKey).(*models.Scene)

	streamManager := manager.GetInstance().StreamManager
	if streamManager == nil {
		http.Error(w, "Live transcoding disabled", http.StatusServiceUnavailable)
		return
	}

	f := scene.Files.Primary()
	if f == nil {
		return
	}

	if err := r.ParseForm(); err != nil {
		logger.Warnf("[transcode] error parsing query form: %v", err)
	}

	resolution := r.Form.Get("resolution")

	ffmpeg.TranscodeDebugf("[transcode] returning %s manifest for scene %d", logName, scene.ID)
	streamManager.ServeManifest(w, r, streamType, f, resolution)
}

func (rs sceneRoutes) streamV3Manifest(w http.ResponseWriter, r *http.Request, streamType *ffmpeg.V3StreamType, logName string) {
	scene := r.Context().Value(sceneKey).(*models.Scene)

	streamManager := manager.GetInstance().StreamManager
	if streamManager == nil {
		http.Error(w, "Live transcoding disabled", http.StatusServiceUnavailable)
		return
	}

	f := scene.Files.Primary()
	if f == nil {
		return
	}

	if trackStr := chi.URLParam(r, "track"); trackStr != "" {
		if _, ok := ffmpeg.ParseTrack(trackStr); !ok {
			http.Error(w, "invalid track", http.StatusBadRequest)
			return
		}
	}

	if err := r.ParseForm(); err != nil {
		logger.Warnf("[transcode] error parsing query form: %v", err)
	}

	resolution := r.Form.Get("resolution")

	ffmpeg.TranscodeDebugf("[transcode] returning %s manifest for scene %d", logName, scene.ID)
	streamManager.ServeV3Manifest(w, r, streamType, f, resolution)
}

func (rs sceneRoutes) StreamV3HLSInitSegment(w http.ResponseWriter, r *http.Request) {
	// The init segment's URL path component is "init"; ParseSegment maps
	// that to -1 (matching the fMP4 segment-type convention).
	rs.streamV3SegmentNamed(w, r, ffmpeg.V3StreamTypeHLS, chi.URLParam(r, "track"), "init")
}

func (rs sceneRoutes) StreamHLSSegment(w http.ResponseWriter, r *http.Request) {
	rs.streamSegment(w, r, ffmpeg.StreamTypeHLS)
}

func (rs sceneRoutes) StreamDASHVideoSegment(w http.ResponseWriter, r *http.Request) {
	rs.streamSegment(w, r, ffmpeg.StreamTypeDASHVideo)
}

func (rs sceneRoutes) StreamDASHAudioSegment(w http.ResponseWriter, r *http.Request) {
	rs.streamSegment(w, r, ffmpeg.StreamTypeDASHAudio)
}

func (rs sceneRoutes) StreamV3HLSSegment(w http.ResponseWriter, r *http.Request) {
	rs.streamV3Segment(w, r, ffmpeg.V3StreamTypeHLS)
}

// StreamHLSCopyFMP4TrackPlaylist serves a per-track media playlist for the
// codec-copy fragmented-MP4 HLS variant. The track (video/audio) is read
// from the {track} URL parameter; segment URLs are derived from the request
// URL by stripping ".m3u8".
func (rs sceneRoutes) StreamV3HLSCopyFMP4TrackPlaylist(w http.ResponseWriter, r *http.Request) {
	rs.streamV3Manifest(w, r, ffmpeg.V3StreamTypeHLSCopyFMP4, "HLS (copy fMP4) track playlist")
}

func (rs sceneRoutes) StreamV3HLSCopyFMP4InitSegment(w http.ResponseWriter, r *http.Request) {
	rs.streamV3SegmentNamed(w, r, ffmpeg.V3StreamTypeHLSCopyFMP4, chi.URLParam(r, "track"), "init")
}

func (rs sceneRoutes) StreamV3HLSCopyFMP4Segment(w http.ResponseWriter, r *http.Request) {
	rs.streamV3Segment(w, r, ffmpeg.V3StreamTypeHLSCopyFMP4)
}

// StreamHLSCopyFMP4AAC* serve the video-copy + AAC-transcode HLS variant.
// Identical segment plumbing to StreamHLSCopyFMP4* — the StreamType is
// what differs, and that drives ffmpeg arg construction (audio re-encode)
// + the cache directory (segments don't share with the full-copy path).
func (rs sceneRoutes) StreamV3HLSCopyFMP4AACMaster(w http.ResponseWriter, r *http.Request) {
	rs.streamV3MasterManifest(w, r, ffmpeg.V3StreamTypeHLSCopyFMP4AAC, "HLS (copy fMP4 + AAC) master")
}

func (rs sceneRoutes) StreamV3HLSCopyFMP4AACTrackPlaylist(w http.ResponseWriter, r *http.Request) {
	rs.streamV3Manifest(w, r, ffmpeg.V3StreamTypeHLSCopyFMP4AAC, "HLS (copy fMP4 + AAC) track playlist")
}

func (rs sceneRoutes) StreamV3HLSCopyFMP4AACInitSegment(w http.ResponseWriter, r *http.Request) {
	rs.streamV3SegmentNamed(w, r, ffmpeg.V3StreamTypeHLSCopyFMP4AAC, chi.URLParam(r, "track"), "init")
}

func (rs sceneRoutes) StreamV3HLSCopyFMP4AACSegment(w http.ResponseWriter, r *http.Request) {
	rs.streamV3Segment(w, r, ffmpeg.V3StreamTypeHLSCopyFMP4AAC)
}

func (rs sceneRoutes) streamSegment(w http.ResponseWriter, r *http.Request, streamType *ffmpeg.StreamType) {
	scene := r.Context().Value(sceneKey).(*models.Scene)

	streamManager := manager.GetInstance().StreamManager
	if streamManager == nil {
		http.Error(w, "Live transcoding disabled", http.StatusServiceUnavailable)
		return
	}

	f := scene.Files.Primary()
	if f == nil {
		return
	}

	if err := r.ParseForm(); err != nil {
		logger.Warnf("[transcode] error parsing query form: %v", err)
	}

	sceneHash := scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm())

	resolution := r.Form.Get("resolution")

	options := ffmpeg.StreamOptions{
		StreamType: streamType,
		VideoFile:  f,
		Resolution: resolution,
		Hash:       sceneHash,
		Segment:    chi.URLParam(r, "segment"),
	}

	streamManager.ServeSegment(w, r, options)
}

func (rs sceneRoutes) streamV3Segment(w http.ResponseWriter, r *http.Request, streamType *ffmpeg.V3StreamType) {
	rs.streamV3SegmentNamed(w, r, streamType, chi.URLParam(r, "track"), chi.URLParam(r, "segment"))
}

func (rs sceneRoutes) streamV3SegmentNamed(w http.ResponseWriter, r *http.Request, streamType *ffmpeg.V3StreamType, trackStr, segment string) {
	track, ok := ffmpeg.ParseTrack(trackStr)
	if !ok {
		http.Error(w, "invalid track", http.StatusBadRequest)
		return
	}

	scene := r.Context().Value(sceneKey).(*models.Scene)

	streamManager := manager.GetInstance().StreamManager
	if streamManager == nil {
		http.Error(w, "Live transcoding disabled", http.StatusServiceUnavailable)
		return
	}

	f := scene.Files.Primary()
	if f == nil {
		return
	}

	if err := r.ParseForm(); err != nil {
		logger.Warnf("[transcode] error parsing query form: %v", err)
	}

	sceneHash := scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm())

	resolution := r.Form.Get("resolution")

	options := ffmpeg.V3StreamOptions{
		StreamType: streamType,
		VideoFile:  f,
		Resolution: resolution,
		Hash:       sceneHash,
		Track:      track,
		Segment:    segment,
	}

	streamManager.ServeV3Segment(w, r, options)
}

// StreamsStop tears down any HLS/transcode v3RunningStream for this
// scene's primary file. Called by the v3 frontend via
// `navigator.sendBeacon` when:
//   - The active source switches from HLS to direct stream (the
//     direct stream path doesn't go through `ServeV3Segment`, so the
//     `ServeV3Segment` sibling-kill never fires for this case).
//   - The active source switches from one HLS variant/resolution to
//     another (the new variant's first segment request would trigger
//     the in-`ServeV3Segment` sibling-kill, but its 2 s grace window
//     leaves the previous stream alive when the swap is fast). The
//     client passes `keep_type` + `keep_resolution` so the new
//     stream's dir is preserved while the old one is reaped
//     immediately.
//   - The player component unmounts (tab close, navigation away).
//
// Without this, the previous HLS transcode lingers for `v3MaxIdleTime`
// (60 s, see `pkg/ffmpeg/stream_v3_segmented.go`). Beacons are fire-and-
// forget, so we return immediately with 204 — the caller (the browser
// firing sendBeacon at unload) doesn't read the body and may have
// already gone away.
func (rs sceneRoutes) StreamsStop(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)

	streamManager := manager.GetInstance().StreamManager
	if streamManager == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	f := scene.Files.Primary()
	if f == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	exceptDir := ""
	keepType := r.URL.Query().Get("keep_type")
	if keepType != "" {
		sceneHash := scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm())
		maxTranscodeSize := config.GetInstance().GetMaxStreamingTranscodeSize().GetMaxResolution()
		if keepResolution := r.URL.Query().Get("keep_resolution"); keepResolution != "" {
			maxTranscodeSize = models.StreamingResolutionEnum(keepResolution).GetMaxResolution()
		}
		// `FileDir` uses only `t.Name`, so a value-only struct is
		// sufficient — no need to look up the canonical pointer.
		exceptDir = ffmpeg.V3StreamType{Name: keepType}.FileDir(sceneHash, maxTranscodeSize)
	}

	streamManager.StopV3StreamsForFile(f.ID, exceptDir)
	w.WriteHeader(http.StatusNoContent)
}

// StreamsKeepalive refreshes the `lastAccessed` timestamp on the
// running HLS transcode identified by `keep_type` + `keep_resolution`
// so it survives the next `v3MaxIdleTime` reap. Called by the v3
// frontend on a ~15 s interval while the HLS player is paused (during
// playback, segment fetches already bump the timestamp).
//
// Returns 204 unconditionally — a missing stream just means it was
// already reaped or hasn't started yet, neither of which is an error
// for a fire-and-forget keepalive.
func (rs sceneRoutes) StreamsKeepalive(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)

	streamManager := manager.GetInstance().StreamManager
	if streamManager == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	keepType := r.URL.Query().Get("keep_type")
	if keepType == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	sceneHash := scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm())
	maxTranscodeSize := config.GetInstance().GetMaxStreamingTranscodeSize().GetMaxResolution()
	if keepResolution := r.URL.Query().Get("keep_resolution"); keepResolution != "" {
		maxTranscodeSize = models.StreamingResolutionEnum(keepResolution).GetMaxResolution()
	}
	dir := ffmpeg.V3StreamType{Name: keepType}.FileDir(sceneHash, maxTranscodeSize)

	streamManager.BumpV3LastAccessed(dir)
	w.WriteHeader(http.StatusNoContent)
}

// DownloadMP4 serves a downloadable MP4 of the scene. Default is "auto"
// mode: codec-copy (preserving AV1 / HEVC / Opus / HDR) when source
// codecs fit MP4 and no resolution downscale was requested; H.264 + AAC
// transcode otherwise. Output is fragmented MP4 streamed live so the
// download starts immediately. PWAs can fetch the same URL through
// Cache API or Background Fetch for offline storage.
func (rs sceneRoutes) DownloadMP4(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)

	streamManager := manager.GetInstance().StreamManager
	if streamManager == nil {
		http.Error(w, "Live transcoding disabled", http.StatusServiceUnavailable)
		return
	}

	f := scene.Files.Primary()
	if f == nil {
		http.Error(w, "scene has no files", http.StatusNotFound)
		return
	}

	if err := r.ParseForm(); err != nil {
		logger.Warnf("[download] error parsing query form: %v", err)
	}

	mode := ffmpeg.ParseDownloadMode(r.Form.Get("mode"))
	resolution := models.StreamingResolutionEnum(r.Form.Get("resolution"))

	// Filename: prefer the scene's title; else the source file's
	// basename. The handler appends `.mp4` itself.
	filename := scene.Title
	if filename == "" {
		filename = strings.TrimSuffix(filepath.Base(f.Path), filepath.Ext(f.Path))
	}

	streamManager.ServeDownload(w, r, ffmpeg.DownloadOptions{
		VideoFile:  f,
		Mode:       mode,
		Resolution: resolution,
		Filename:   filename,
	})
}

func (rs sceneRoutes) Screenshot(w http.ResponseWriter, r *http.Request) {
	// if default flag is set, return the default image
	if r.URL.Query().Get("default") == "true" {
		utils.ServeImage(w, r, static.ReadAll(static.DefaultSceneImage))
		return
	}

	scene := r.Context().Value(sceneKey).(*models.Scene)

	ss := manager.SceneServer{
		TxnManager:       rs.txnManager,
		SceneCoverGetter: rs.sceneFinder,
	}
	ss.ServeScreenshot(scene, w, r)
}

func (rs sceneRoutes) Preview(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	sceneHash := scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm())
	filepath := manager.GetInstance().Paths.Scene.GetVideoPreviewPath(sceneHash)

	utils.ServeStaticFile(w, r, filepath)
}

func (rs sceneRoutes) Webp(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	sceneHash := scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm())
	filepath := manager.GetInstance().Paths.Scene.GetWebpPreviewPath(sceneHash)

	utils.ServeStaticFile(w, r, filepath)
}

func (rs sceneRoutes) getChapterVttTitle(r *http.Request, marker *models.SceneMarker) (*string, error) {
	if marker.Title != "" {
		return &marker.Title, nil
	}

	var title string
	if err := rs.withReadTxn(r, func(ctx context.Context) error {
		qb := rs.tagFinder
		primaryTag, err := qb.Find(ctx, marker.PrimaryTagID)
		if err != nil {
			return err
		}

		title = primaryTag.Name

		tags, err := qb.FindBySceneMarkerID(ctx, marker.ID)
		if err != nil {
			return err
		}

		for _, t := range tags {
			title += ", " + t.Name
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return &title, nil
}

func (rs sceneRoutes) VttChapter(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	var sceneMarkers []*models.SceneMarker
	readTxnErr := rs.withReadTxn(r, func(ctx context.Context) error {
		var err error
		sceneMarkers, err = rs.sceneMarkerFinder.FindBySceneID(ctx, scene.ID)
		return err
	})
	if errors.Is(readTxnErr, context.Canceled) {
		return
	}
	if readTxnErr != nil {
		logger.Warnf("read transaction error on fetch scene markers: %v", readTxnErr)
		http.Error(w, readTxnErr.Error(), http.StatusInternalServerError)
		return
	}

	vttLines := []string{"WEBVTT", ""}
	for i, marker := range sceneMarkers {
		vttLines = append(vttLines, strconv.Itoa(i+1))
		time := utils.GetVTTTime(marker.Seconds)
		vttLines = append(vttLines, time+" --> "+time)

		vttTitle, err := rs.getChapterVttTitle(r, marker)
		if errors.Is(err, context.Canceled) {
			return
		}
		if err != nil {
			logger.Warnf("read transaction error on fetch scene marker title: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		vttLines = append(vttLines, *vttTitle)
		vttLines = append(vttLines, "")
	}
	vtt := strings.Join(vttLines, "\n")

	w.Header().Set("Content-Type", "text/vtt")
	utils.ServeStaticContent(w, r, []byte(vtt))
}

func (rs sceneRoutes) VttThumbs(w http.ResponseWriter, r *http.Request) {
	scene, ok := r.Context().Value(sceneKey).(*models.Scene)
	var sceneHash string
	if ok && scene != nil {
		sceneHash = scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm())
	} else {
		sceneHash = chi.URLParam(r, "sceneHash")
	}
	filepath := manager.GetInstance().Paths.Scene.GetSpriteVttFilePath(sceneHash)

	w.Header().Set("Content-Type", "text/vtt")
	utils.ServeStaticFile(w, r, filepath)
}

func (rs sceneRoutes) VttSprite(w http.ResponseWriter, r *http.Request) {
	scene, ok := r.Context().Value(sceneKey).(*models.Scene)
	var sceneHash string
	if ok && scene != nil {
		sceneHash = scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm())
	} else {
		sceneHash = chi.URLParam(r, "sceneHash")
	}
	filepath := manager.GetInstance().Paths.Scene.GetSpriteImageFilePath(sceneHash)

	utils.ServeStaticFile(w, r, filepath)
}

func (rs sceneRoutes) Funscript(w http.ResponseWriter, r *http.Request) {
	s := r.Context().Value(sceneKey).(*models.Scene)
	filepath := video.GetFunscriptPath(s.Path)

	utils.ServeStaticFile(w, r, filepath)
}

func (rs sceneRoutes) InteractiveCSV(w http.ResponseWriter, r *http.Request) {
	s := r.Context().Value(sceneKey).(*models.Scene)
	filepath := video.GetFunscriptPath(s.Path)

	// TheHandy directly only accepts interactive CSVs
	csvBytes, err := manager.ConvertFunscriptToCSV(filepath)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utils.ServeStaticContent(w, r, csvBytes)
}

func (rs sceneRoutes) InteractiveHeatmap(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	sceneHash := scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm())
	filepath := manager.GetInstance().Paths.Scene.GetInteractiveHeatmapPath(sceneHash)

	utils.ServeStaticFile(w, r, filepath)
}

func (rs sceneRoutes) Caption(w http.ResponseWriter, r *http.Request, lang string, ext string) {
	s := r.Context().Value(sceneKey).(*models.Scene)

	var captions []*models.VideoCaption
	readTxnErr := rs.withReadTxn(r, func(ctx context.Context) error {
		var err error
		primaryFile := s.Files.Primary()
		if primaryFile == nil {
			return nil
		}

		captions, err = rs.captionFinder.GetCaptions(ctx, primaryFile.Base().ID)

		return err
	})
	if errors.Is(readTxnErr, context.Canceled) {
		return
	}
	if readTxnErr != nil {
		logger.Warnf("read transaction error on fetch scene captions: %v", readTxnErr)
		http.Error(w, readTxnErr.Error(), http.StatusInternalServerError)
		return
	}

	for _, caption := range captions {
		if lang != caption.LanguageCode || ext != caption.CaptionType {
			continue
		}

		sub, err := video.ReadSubs(caption.Path(s.Path))
		if err != nil {
			logger.Warnf("error while reading subs: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var buf bytes.Buffer

		err = sub.WriteToWebVTT(&buf)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/vtt")
		utils.ServeStaticContent(w, r, buf.Bytes())
		return
	}
}

func (rs sceneRoutes) CaptionLang(w http.ResponseWriter, r *http.Request) {
	// serve caption based on lang query param, if provided
	if err := r.ParseForm(); err != nil {
		logger.Warnf("[caption] error parsing query form: %v", err)
	}

	l := r.Form.Get("lang")
	ext := r.Form.Get("type")
	rs.Caption(w, r, l, ext)
}

func (rs sceneRoutes) SceneMarkerStream(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	sceneHash := scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm())
	sceneMarkerID, _ := strconv.Atoi(chi.URLParam(r, "sceneMarkerId"))
	var sceneMarker *models.SceneMarker
	readTxnErr := rs.withReadTxn(r, func(ctx context.Context) error {
		var err error
		sceneMarker, err = rs.sceneMarkerFinder.Find(ctx, sceneMarkerID)
		return err
	})
	if errors.Is(readTxnErr, context.Canceled) {
		return
	}
	if readTxnErr != nil {
		logger.Warnf("read transaction error on fetch scene marker: %v", readTxnErr)
		http.Error(w, readTxnErr.Error(), http.StatusInternalServerError)
		return
	}

	if sceneMarker == nil {
		http.Error(w, http.StatusText(404), 404)
		return
	}

	filepath := manager.GetInstance().Paths.SceneMarkers.GetVideoPreviewPath(sceneHash, int(sceneMarker.Seconds))
	utils.ServeStaticFile(w, r, filepath)
}

func (rs sceneRoutes) SceneMarkerPreview(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	sceneHash := scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm())
	sceneMarkerID, _ := strconv.Atoi(chi.URLParam(r, "sceneMarkerId"))
	var sceneMarker *models.SceneMarker
	readTxnErr := rs.withReadTxn(r, func(ctx context.Context) error {
		var err error
		sceneMarker, err = rs.sceneMarkerFinder.Find(ctx, sceneMarkerID)
		return err
	})
	if errors.Is(readTxnErr, context.Canceled) {
		return
	}
	if readTxnErr != nil {
		logger.Warnf("read transaction error on fetch scene marker preview: %v", readTxnErr)
		http.Error(w, readTxnErr.Error(), http.StatusInternalServerError)
		return
	}

	if sceneMarker == nil {
		http.Error(w, http.StatusText(404), 404)
		return
	}

	filepath := manager.GetInstance().Paths.SceneMarkers.GetWebpPreviewPath(sceneHash, int(sceneMarker.Seconds))

	// If the image doesn't exist, send the placeholder
	exists, _ := fsutil.FileExists(filepath)
	if !exists {
		w.Header().Set("Content-Type", "image/png")
		utils.ServeStaticContent(w, r, utils.PendingGenerateResource)
	} else {
		utils.ServeStaticFile(w, r, filepath)
	}
}

func (rs sceneRoutes) SceneMarkerScreenshot(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	sceneHash := scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm())
	sceneMarkerID, _ := strconv.Atoi(chi.URLParam(r, "sceneMarkerId"))
	var sceneMarker *models.SceneMarker
	readTxnErr := rs.withReadTxn(r, func(ctx context.Context) error {
		var err error
		sceneMarker, err = rs.sceneMarkerFinder.Find(ctx, sceneMarkerID)
		return err
	})
	if errors.Is(readTxnErr, context.Canceled) {
		return
	}
	if readTxnErr != nil {
		logger.Warnf("read transaction error on fetch scene marker screenshot: %v", readTxnErr)
		http.Error(w, readTxnErr.Error(), http.StatusInternalServerError)
		return
	}

	if sceneMarker == nil {
		http.Error(w, http.StatusText(404), 404)
		return
	}

	filepath := manager.GetInstance().Paths.SceneMarkers.GetScreenshotPath(sceneHash, int(sceneMarker.Seconds))

	// If the image doesn't exist, send the placeholder
	exists, _ := fsutil.FileExists(filepath)
	if !exists {
		w.Header().Set("Content-Type", "image/png")
		utils.ServeStaticContent(w, r, utils.PendingGenerateResource)
	} else {
		utils.ServeStaticFile(w, r, filepath)
	}
}

func (rs sceneRoutes) SceneCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sceneID, err := strconv.Atoi(chi.URLParam(r, "sceneId"))
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		var scene *models.Scene
		_ = rs.withReadTxn(r, func(ctx context.Context) error {
			qb := rs.sceneFinder
			scene, _ = qb.Find(ctx, sceneID)

			if scene != nil {
				if err := scene.LoadPrimaryFile(ctx, rs.fileGetter); err != nil {
					if !errors.Is(err, context.Canceled) {
						logger.Errorf("error loading primary file for scene %d: %v", sceneID, err)
					}
					// set scene to nil so that it doesn't try to use the primary file
					scene = nil
				}
			}

			return nil
		})
		if scene == nil {
			http.Error(w, http.StatusText(404), 404)
			return
		}

		ctx := context.WithValue(r.Context(), sceneKey, scene)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
