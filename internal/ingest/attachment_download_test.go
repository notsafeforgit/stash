package ingest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func downloadEnvelopeFixture() AttachmentDownloadEvent {
	return AttachmentDownloadEvent{Protocol: ProtocolVersion, ProducerUUID: uuid.NewString(), EventUUID: uuid.NewString(),
		RunUUID: uuid.NewString(), CollectionUUID: uuid.NewString(), CollectionRevision: 3, RootUUID: uuid.NewString(), Kind: "attachment.download",
		ObservedAt: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), OwnerUUID: uuid.NewString(), Fence: 2, TransferSequence: 17,
		CaptureEventUUID: uuid.NewString(), Attachment: PostReference{Namespace: "native:reddit", Value: "media-1"}, State: "started"}
}

func TestAttachmentDownloadEnvelopeKeepsReportedTransfersDistinctFromVerification(t *testing.T) {
	for _, state := range []string{"started", "downloaded", "failed", "excluded", "skipped"} {
		t.Run(state, func(t *testing.T) {
			event := downloadEnvelopeFixture()
			event.State = state
			switch state {
			case "downloaded":
				event.FileEventUUID = uuid.NewString()
			case "failed":
				event.ReasonCode = "download_failed"
			case "excluded":
				event.ReasonCode = "unsupported_media"
			case "skipped":
				event.ReasonCode = "archive_entry_without_file"
			}
			body, err := json.Marshal(event)
			require.NoError(t, err)
			decoded, err := decodeAttachmentDownload(body)
			require.NoError(t, err)
			require.Equal(t, event, *decoded)
		})
	}
}

func TestAttachmentDownloadEnvelopeRejectsUnboundOrContradictoryReports(t *testing.T) {
	cases := map[string]func(*AttachmentDownloadEvent){
		"missing capture":        func(e *AttachmentDownloadEvent) { e.CaptureEventUUID = "" },
		"missing owner":          func(e *AttachmentDownloadEvent) { e.OwnerUUID = "" },
		"missing root":           func(e *AttachmentDownloadEvent) { e.RootUUID = "" },
		"missing fence":          func(e *AttachmentDownloadEvent) { e.Fence = 0 },
		"missing transfer order": func(e *AttachmentDownloadEvent) { e.TransferSequence = 0 },
		"inexact transfer order": func(e *AttachmentDownloadEvent) { e.TransferSequence = 1 << 53 },
		"unknown attachment":     func(e *AttachmentDownloadEvent) { e.Attachment.Value = "" },
		"control attachment":     func(e *AttachmentDownloadEvent) { e.Attachment.Value = "media\n1" },
		"missing completed file": func(e *AttachmentDownloadEvent) { e.State = "downloaded" },
		"failed with completed file": func(e *AttachmentDownloadEvent) {
			e.State = "failed"
			e.ReasonCode = "download_failed"
			e.FileEventUUID = uuid.NewString()
		},
		"untyped error details": func(e *AttachmentDownloadEvent) {
			e.State = "failed"
			e.ReasonCode = "https://source.invalid/?token=secret"
		},
		"unknown exclusion":                    func(e *AttachmentDownloadEvent) { e.State = "excluded"; e.ReasonCode = "network_failure" },
		"producer cannot declare availability": func(e *AttachmentDownloadEvent) { e.State = "available" },
		"producer cannot declare expiry":       func(e *AttachmentDownloadEvent) { e.State = "interrupted" },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			event := downloadEnvelopeFixture()
			edit(&event)
			body, err := json.Marshal(event)
			require.NoError(t, err)
			_, err = decodeAttachmentDownload(body)
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestAttachmentDownloadEnvelopeUsesStrictBoundedInput(t *testing.T) {
	body, err := json.Marshal(downloadEnvelopeFixture())
	require.NoError(t, err)
	for _, invalid := range []string{
		strings.Replace(string(body), `"protocol":1`, `"protocol":1,"protocol":1`, 1),
		strings.TrimSuffix(string(body), "}") + `,"password":"secret"}`,
		string(body) + strings.Repeat(" ", MaxAttachmentDownloadBytes),
	} {
		_, err := decodeAttachmentDownload([]byte(invalid))
		require.Error(t, err)
	}
	event := downloadEnvelopeFixture()
	event.Protocol++
	body, err = json.Marshal(event)
	require.NoError(t, err)
	_, err = decodeAttachmentDownload(body)
	require.ErrorIs(t, err, ErrUnsupported)
}
