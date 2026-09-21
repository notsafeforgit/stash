package ffmpeg

import "context"

type privateV3StreamKey struct{}

// WithPrivateV3Stream strips source container/track metadata from a guest's
// rendition. Its caller must also assign a separate, server-owned session ID.
func WithPrivateV3Stream(ctx context.Context) context.Context {
	return context.WithValue(ctx, privateV3StreamKey{}, true)
}

func privateV3Stream(ctx context.Context) bool {
	value, _ := ctx.Value(privateV3StreamKey{}).(bool)
	return value
}

func privateV3MetadataArgs() Args {
	return Args{"-map_metadata", "-1", "-map_metadata:s", "-1", "-map_chapters", "-1"}
}
