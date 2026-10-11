package sourceadapter

import "github.com/stashapp/stash/pkg/jsonutil"

// CapturedContextPath is the payload location used for the post and publisher.
// Feed/profile context does not replace a social post's own publisher.
func CapturedContextPath(raw []byte) (string, error) {
	data, err := jsonutil.DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil {
		return "", err
	}
	_, path, _, err := capturedSourceContext(data)
	return path, err
}
