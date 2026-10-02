package archive_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestRetainedDocumentSharesBytesWithoutReplacingParserEvidence(t *testing.T) {
	input := models.SourceDocumentInput{Content: []byte{0xff, '<', 'x', '>'}, Encoding: "unknown", Parser: "legacy-catalog-nfo-v1", ParseStatus: "invalid",
		Warnings: json.RawMessage(`["Undecodable original bytes"]`), Parsed: json.RawMessage(`{"id":[90071992547409931234]}`)}
	doc, err := archive.PrepareSourceDocument(input)
	require.NoError(t, err)
	repeat, err := archive.PrepareSourceDocument(input)
	require.NoError(t, err)
	require.Equal(t, doc, repeat)
	input.ParseStatus = "repaired"
	repaired, err := archive.PrepareSourceDocument(input)
	require.NoError(t, err)
	require.Equal(t, doc.ContentSHA256, repaired.ContentSHA256)
	require.NotEqual(t, doc.UUID, repaired.UUID)
	input.Parsed = json.RawMessage(`{ "id": [90071992547409931234] }`)
	spelled, err := archive.PrepareSourceDocument(input)
	require.NoError(t, err)
	require.NotEqual(t, repaired.UUID, spelled.UUID, "original parser serialization remains recoverable")
	input.Parsed[0] = '['
	require.Equal(t, json.RawMessage(`{ "id": [90071992547409931234] }`), spelled.Parsed, "prepared records own their bytes")
}

func TestRetainedDocumentRejectsAmbiguousOrUnboundedInterpretations(t *testing.T) {
	good := models.SourceDocumentInput{Content: []byte{}, Parser: "fixture", ParseStatus: "empty", Warnings: json.RawMessage(`[]`), Parsed: json.RawMessage(`{}`)}
	for _, change := range []func(*models.SourceDocumentInput){
		func(v *models.SourceDocumentInput) { v.Content = bytes.Repeat([]byte{'x'}, archive.MaxDocumentBytes+1) },
		func(v *models.SourceDocumentInput) { v.Parser = "" },
		func(v *models.SourceDocumentInput) { v.ParseStatus = "bad\x00status" },
		func(v *models.SourceDocumentInput) { v.Warnings = json.RawMessage(`{}`) },
		func(v *models.SourceDocumentInput) { v.Warnings = json.RawMessage(`["\ud800"]`) },
		func(v *models.SourceDocumentInput) { v.Parsed = json.RawMessage(`[]`) },
		func(v *models.SourceDocumentInput) { v.Parsed = json.RawMessage(`{"title":1,"title":2}`) },
		func(v *models.SourceDocumentInput) { v.Parsed = json.RawMessage(`{"value":NaN}`) },
	} {
		input := good
		change(&input)
		_, err := archive.PrepareSourceDocument(input)
		require.ErrorIs(t, err, models.ErrSourceDocumentInvalid)
	}
	require.True(t, archive.ValidDocumentPath("folder\\literal/path.nfo"), "retained paths are not instructions to open a filesystem location")
	require.False(t, archive.ValidDocumentPath(""))
	require.False(t, archive.ValidDocumentPath("a\x00b"))
	require.True(t, archive.ValidDocumentSourceTime(""), "unknown source times stay unknown")
	require.True(t, archive.ValidDocumentSourceTime("2026-09-29T02:03:04.123456789-07:00"))
	require.False(t, archive.ValidDocumentSourceTime("2026-09-29"))
}
