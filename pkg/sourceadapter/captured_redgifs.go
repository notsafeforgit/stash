package sourceadapter

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

var redgifsItemID = regexp.MustCompile(`^[A-Za-z0-9]{1,256}$`)

func capturedRedgifsPost(data sourceObject, path string) (*models.SourcePostIdentifier, error) {
	id, ok := data["id"].(string)
	if !ok || !redgifsItemID.MatchString(id) {
		return nil, errors.New("Redgifs item requires its API ID")
	}
	return capturedPostReference("native:redgifs", capturedField{value: strings.ToLower(id), path: pointerMember(path, "id")})
}

// Each API item has its own watch permalink. A gallery label or downloader
// count does not establish the membership or order of the enclosing gallery.
func capturedRedgifsAlbum(data sourceObject, path string) (*CapturedAlbum, error) {
	post, err := capturedRedgifsPost(data, path)
	if err != nil {
		return nil, err
	}
	urls, ok := data["urls"].(sourceObject)
	if !ok {
		return nil, nil
	}
	usable := false
	for _, key := range []string{"hd", "sd", "gif"} {
		u := sourceURL(urls[key])
		usable = usable || u != nil && u.User == nil && u.Port() == ""
	}
	if !usable {
		return nil, nil
	}
	result := &CapturedAlbum{Policy: CapturedAlbumPolicy, Post: *post, EvidencePath: pointerMember(path, "id"),
		Manifest: models.SourceAttachmentManifestInput{Complete: !sourceTruthy(data["gallery"]),
			Entries: []models.SourceAttachmentEntry{{Position: 0, Reference: *post, MediaKind: "unknown"}}}}
	if result.Manifest.Complete {
		count := 1
		result.Manifest.ExpectedCount = &count
	}
	return result, nil
}

func capturedRedgifsPublication(data sourceObject) (*string, error) {
	if data["createDate"] == nil {
		return nil, nil
	}
	number, ok := data["createDate"].(json.Number)
	if !ok {
		return nil, errors.New("invalid Redgifs publication timestamp")
	}
	seconds, err := number.Int64()
	if err != nil || seconds < 0 || seconds >= 253402300800 {
		return nil, errors.New("invalid Redgifs publication timestamp")
	}
	value := time.Unix(seconds, 0).UTC().Format("2006-01-02T15:04:05.000Z")
	return &value, nil
}
