package ffmpeg

import (
	"context"
	"net/http"
)

type privateV3StreamKey struct{}

// WithPrivateV3Stream assigns a server-owned encoder session to a guest. The
// request URL keeps the browser's session so generated playlists preserve that
// identity without exposing or sharing the owner's encoder sessions.
func WithPrivateV3Stream(ctx context.Context, session string) context.Context {
	return context.WithValue(ctx, privateV3StreamKey{}, session)
}

// V3StreamSessionFromRequest resolves the internal encoder identity without
// putting it into public playlists. Ordinary players retain their URL session.
func V3StreamSessionFromRequest(r *http.Request) (string, error) {
	if session, ok := r.Context().Value(privateV3StreamKey{}).(string); ok {
		return ParseV3StreamSession(session)
	}
	return ParseV3StreamSession(r.URL.Query().Get("stream_session"))
}
