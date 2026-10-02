package archive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

const MaxDocumentBytes = 16 << 20

func documentText(value string, limit int, empty bool) bool {
	return (empty || value != "") && len(value) <= limit && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func ValidDocumentPath(value string) bool { return documentText(value, 8192, false) }

func ValidDocumentSourceTime(value string) bool {
	if value == "" {
		return true
	}
	_, err := time.Parse(time.RFC3339Nano, value)
	return len(value) <= 64 && err == nil
}

// The identity includes exact parser evidence. Different interpretations share
// content bytes but remain distinct documents; migration never reruns a parser.
func DocumentIdentity(doc models.SourceDocument) (string, error) {
	if !ValidSHA256(doc.ContentSHA256) || doc.ByteSize < 0 || doc.ByteSize > MaxDocumentBytes ||
		!documentText(doc.Encoding, 128, true) || !documentText(doc.Parser, 128, false) ||
		!documentText(doc.ParseStatus, 128, false) || len(doc.Warnings) > 4<<20 || len(doc.Parsed) > 4<<20 {
		return "", models.ErrSourceDocumentInvalid
	}
	if !utf8.Valid(doc.Warnings) || !json.Valid(doc.Warnings) || !validJSONSurrogates(doc.Warnings) {
		return "", models.ErrSourceDocumentInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(doc.Warnings))
	decoder.UseNumber()
	warnings, err := decodeSourceJSON(decoder, 0)
	if err != nil {
		return "", models.ErrSourceDocumentInvalid
	}
	if _, ok := warnings.([]any); !ok {
		return "", models.ErrSourceDocumentInvalid
	}
	if _, err := DecodeJSONObject(doc.Parsed, 4<<20); err != nil {
		return "", models.ErrSourceDocumentInvalid
	}
	value, err := json.Marshal([]string{doc.ContentSHA256, doc.Encoding, doc.Parser, doc.ParseStatus, string(doc.Warnings), string(doc.Parsed)})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(value)
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("urn:stash:source-document:v1:"+hex.EncodeToString(sum[:]))).String(), nil
}

func PrepareSourceDocument(input models.SourceDocumentInput) (*models.SourceDocument, error) {
	if len(input.Content) > MaxDocumentBytes {
		return nil, models.ErrSourceDocumentInvalid
	}
	sum := sha256.Sum256(input.Content)
	ret := &models.SourceDocument{ContentSHA256: hex.EncodeToString(sum[:]), ByteSize: int64(len(input.Content)), Encoding: input.Encoding,
		Parser: input.Parser, ParseStatus: input.ParseStatus, Warnings: append(json.RawMessage(nil), input.Warnings...), Parsed: append(json.RawMessage(nil), input.Parsed...)}
	var err error
	ret.UUID, err = DocumentIdentity(*ret)
	return ret, err
}
