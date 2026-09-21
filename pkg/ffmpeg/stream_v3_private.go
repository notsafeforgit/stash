package ffmpeg

import (
	"context"
	"net/http"
)

type privateV3StreamKey struct{}

// WithPrivateV3Stream strips source container/track metadata from a guest's
// rendition and assigns a server-owned encoder session. The request URL keeps
// the browser's session so generated playlist URLs preserve the same identity.
func WithPrivateV3Stream(ctx context.Context, session string) context.Context {
	return context.WithValue(ctx, privateV3StreamKey{}, session)
}

func privateV3Stream(ctx context.Context) bool {
	_, ok := ctx.Value(privateV3StreamKey{}).(string)
	return ok
}

// V3StreamSessionFromRequest resolves the internal encoder identity without
// putting it into public playlists. Ordinary players retain their URL session.
func V3StreamSessionFromRequest(r *http.Request) (string, error) {
	if session, ok := r.Context().Value(privateV3StreamKey{}).(string); ok {
		return ParseV3StreamSession(session)
	}
	return ParseV3StreamSession(r.URL.Query().Get("stream_session"))
}

func privateV3MetadataArgs() Args {
	return Args{"-map_metadata", "-1", "-map_metadata:s", "-1", "-map_chapters", "-1"}
}
