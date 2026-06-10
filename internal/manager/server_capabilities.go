package manager

import "github.com/stashapp/stash/internal/manager/config"

// ServerCapabilities is the static, server-determined capability flag
// set surfaced to the v3 client via the `serverCapabilities` Query
// field. Probed at server startup and treated as constant for the
// process lifetime — no live config tracking, since the underlying
// signals (which encoders ffmpeg was built with) don't change without
// restarting the server.
//
// Mirrors `graphql/schema/types/metadata.graphql:ServerCapabilities`.
type ServerCapabilities struct {
	DownloadFormats []string `gqlgen:"downloadFormats"`
}

// GetServerCapabilities computes the capability set on demand. Cheap
// (a few map lookups against the cached HW-encoder probe inside
// FFMpeg) so we don't memoise; if profiling shows it appearing in a
// hot path, cache on the Manager.
func (s *Manager) GetServerCapabilities() *ServerCapabilities {
	if !config.GetInstance().GetEnableV3UI() {
		return &ServerCapabilities{}
	}

	formats := []string{"auto", "copy", "copy-aac", "h264"}
	if s.FFMpeg != nil && s.FFMpeg.HasHWHEVCEncoder() {
		// HEVC is gated on HW encoder availability — see
		// `FFMpeg.HasHWHEVCEncoder` for the rationale (libx265 at 4K
		// is too slow to be a sensible auto-pick for download UX).
		// The download endpoint still accepts `mode=hevc` and falls
		// through to libx265 for callers that explicitly opt in.
		formats = append(formats, "hevc")
	}
	if s.FFMpeg != nil && s.FFMpeg.HasHWAV1Encoder() {
		// AV1 only when HW encoding is available — there's no CPU
		// fallback because libsvtav1 at typical scene resolutions is
		// too slow for an offline-download flow. Encode hardware is
		// rare (Intel Arc / Meteor Lake+, RTX 40-series+).
		formats = append(formats, "av1")
	}
	return &ServerCapabilities{
		DownloadFormats: formats,
	}
}
