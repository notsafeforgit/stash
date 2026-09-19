package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestV3EntityImageRejectsHTML(t *testing.T) {
	html := []byte("<!DOCTYPE html><html>not an image</html>")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(html)
	}))
	defer server.Close()
	for _, input := range []string{
		"data:image/png;base64," + base64.StdEncoding.EncodeToString(html),
		server.URL,
	} {
		r := &mutationResolver{}
		data, set, err := r.processEntityImageInputObject(context.Background(), &models.EntityImageInput{Data: &input})
		require.ErrorContains(t, err, "unsupported image content type")
		require.True(t, set)
		require.Empty(t, data)
	}
}
