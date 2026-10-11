package archive

import (
	"encoding/json"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sourceadapter"
)

// Source adapters interpret external formats without reading or changing the
// library. This boundary keeps their claims separate from core validation and
// from the transactions that choose accounts, attachments and media.
const SourceRetentionVersion = sourceadapter.SourceRetentionVersion
const PostContentRetentionVersion = sourceadapter.PostContentRetentionVersion
const MaxSourcePayloadBytes = models.MaxSourcePayloadBytes
const CapturedAccountPolicy = sourceadapter.CapturedAccountPolicy
const CapturedAlbumPolicy = sourceadapter.CapturedAlbumPolicy

type CapturedAccount = models.CapturedAccount
type CapturedAccountIdentifier = models.CapturedAccountIdentifier
type CapturedAlbum = sourceadapter.CapturedAlbum

func ValidAccountNamespace(namespace string) bool {
	return sourceadapter.ValidAccountNamespace(namespace)
}

func NormalizeAccountReference(ref models.AccountReference) (models.AccountReference, error) {
	return sourceadapter.NormalizeAccountReference(ref)
}

// ProfileReference normalizes a locator; it does not establish ownership.
func ProfileReference(value string) *models.AccountReference {
	return sourceadapter.ProfileReference(value)
}

func ProfileReferences(value string) []models.AccountReference {
	return sourceadapter.ProfileReferences(value)
}

func CanonicalProfileURL(value string) (string, bool) {
	return sourceadapter.CanonicalProfileURL(value)
}

func ProfileURLKey(value string) string {
	return sourceadapter.ProfileURLKey(value)
}

func ExtractCapturedAccount(raw []byte) (*CapturedAccount, error) {
	return sourceadapter.ExtractCapturedAccount(raw)
}

func CapturedAccountReferences(raw []byte) (*CapturedAccount, error) {
	return sourceadapter.CapturedAccountReferences(raw)
}

func ExtractCapturedProfileURLs(raw []byte) ([]string, error) {
	return sourceadapter.ExtractCapturedProfileURLs(raw)
}

func ExtractCapturedPost(raw []byte) (*models.SourcePostIdentifier, error) {
	return sourceadapter.ExtractCapturedPost(raw)
}

func CapturedPostPlatform(post models.SourcePostIdentifier) string {
	return sourceadapter.CapturedPostPlatform(post)
}

// ExtractCapturedAlbum validates the adapter's source-list claims using the
// same manifest rules as manual/API input. Parsing cannot bypass count, size,
// identity, media-kind or position checks, nor choose a stored association.
func ExtractCapturedAlbum(raw []byte) (*CapturedAlbum, error) {
	result, err := sourceadapter.ExtractCapturedAlbum(raw)
	if err != nil || result == nil {
		return nil, err
	}
	result.Manifest, err = NormalizeAttachmentManifest(result.Manifest)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func ExtractCapturedThread(raw []byte) (*models.SourceThreadFacts, error) {
	return sourceadapter.ExtractCapturedThread(raw)
}

func TwitterThreadID(value string) bool {
	return sourceadapter.TwitterThreadID(value)
}

func CapturedMetadata(raw []byte) (models.SourcePostMetadata, error) {
	return sourceadapter.CapturedMetadata(raw)
}

func CapturedContextPath(raw []byte) (string, error) {
	return sourceadapter.CapturedContextPath(raw)
}

func RetainSourcePayload(raw []byte) (json.RawMessage, error) {
	return sourceadapter.RetainSourcePayload(raw)
}

func RetainPostContent(raw []byte) (json.RawMessage, error) {
	return sourceadapter.RetainPostContent(raw)
}
