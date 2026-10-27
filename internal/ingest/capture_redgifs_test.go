package ingest_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestStandaloneRedgifsCaptureReplayAndAttachment(t *testing.T) {
	f := newCaptureFixtureForNamespace(t, "native:redgifs")
	event := f.event(t)
	event.Post = ingest.PostReference{Namespace: "native:redgifs", Value: "exampleclip"}
	var err error
	event.Source, err = archive.RetainSourcePayload([]byte(`{"category":"redgifs","id":"ExampleClip","userName":"Publisher","createDate":1768092373,"description":"Original description","urls":{"hd":"https://media.redgifs.com/ExampleClip.mp4"},"_url":"https://media.redgifs.com/ExampleClip.mp4"}`))
	require.NoError(t, err)
	event.Metadata, err = archive.CapturedMetadata(event.Source)
	require.NoError(t, err)
	first, err := f.submit(t, event)
	require.NoError(t, err)
	replay, err := f.submit(t, event)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	var result ingest.CaptureResult
	require.NoError(t, json.Unmarshal(first.Result, &result))
	require.Equal(t, "committed", result.Status)
	require.Equal(t, "unavailable", result.Publisher, "handle-only publishers still require explicit review")
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		selection, err := f.service.Repo.SourceAttachment.Selection(ctx, first.PostUUID)
		require.NoError(t, err)
		require.True(t, selection.Complete)
		require.Equal(t, 1, *selection.ExpectedCount)
		require.Len(t, selection.Entries, 1)
		require.Equal(t, models.SourcePostIdentifier{Namespace: "native:redgifs", Value: "exampleclip"}, selection.Entries[0].Attachment.Reference)
		return nil
	}))
}
