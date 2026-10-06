package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

// Reuse the real CLI/host/n8n worker fixture, including its delayed reports and
// portable restore. The read routes must expose one transfer while leaving the
// original two-report history and queued file-verification result intact.
func verifyAttachmentDownloadTransferHTTP(t *testing.T, repo models.Repository, attachment, start, end, file string) {
	t.Helper()
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	request := func(method, path string, body []byte, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		return w
	}
	path := "/attachments/" + attachment + "/download-transfers"
	var page models.AttachmentDownloadPage
	require.NoError(t, json.Unmarshal(request(http.MethodGet, path+"?limit=1", nil, 200).Body.Bytes(), &page))
	require.Equal(t, attachment, page.AttachmentUUID)
	require.Len(t, page.Transfers, 1)
	require.Nil(t, page.NextBefore)
	require.False(t, page.CheckedAt.IsZero())
	row := page.Transfers[0]
	require.Equal(t, attachment, row.AttachmentUUID)
	require.Equal(t, start, row.StartEventUUID)
	require.Equal(t, end, row.TerminalEventUUID)
	require.Equal(t, file, row.FileEventUUID)
	require.Equal(t, "downloaded", row.State)
	require.Equal(t, "queued", row.VerificationState)
	require.NotEmpty(t, row.VerificationJob)
	require.NotNil(t, row.StartedAt)
	require.NotNil(t, row.FinishedAt)
	require.NotEmpty(t, row.CollectionLabel)
	require.NotEmpty(t, row.RootLabel)
	require.NoError(t, json.Unmarshal(request(http.MethodGet, path+"?before="+strconv.FormatInt(row.Sequence, 10), nil, 200).Body.Bytes(), &page))
	require.Empty(t, page.Transfers)
	require.Nil(t, page.NextBefore)
	for _, query := range []string{"before=0", "before=-1", "before=9007199254740992", "before=01", "limit=0", "limit=26", "after=1", "limit=1&limit=2"} {
		request(http.MethodGet, path+"?"+query, nil, 400)
	}
	request(http.MethodGet, "/attachments/invalid/download-transfers", nil, 400)
	request(http.MethodGet, "/attachments/"+uuid.NewString()+"/download-transfers", nil, 404)
	body := []byte(fmt.Sprintf(`{"attachments":[%q]}`, attachment))
	var status struct {
		CheckedAt   time.Time `json:"checked_at"`
		Attachments []struct {
			AttachmentUUID string                             `json:"attachment_uuid"`
			Latest         *models.AttachmentDownloadTransfer `json:"latest"`
		} `json:"attachments"`
	}
	require.NoError(t, json.Unmarshal(request(http.MethodPost, "/attachments/download-status", body, 200).Body.Bytes(), &status))
	require.Len(t, status.Attachments, 1)
	require.Equal(t, attachment, status.Attachments[0].AttachmentUUID)
	require.Equal(t, &row, status.Attachments[0].Latest)
	require.False(t, status.CheckedAt.IsZero())
	for _, body := range []string{`{}`, `{"attachments":[]}`, `{"attachments":["bad"]}`, fmt.Sprintf(`{"attachments":[%q,%q]}`, attachment, attachment),
		fmt.Sprintf(`{"attachments":[%q],"apply":true}`, attachment)} {
		request(http.MethodPost, "/attachments/download-status", []byte(body), 400)
	}
	ids := make([]string, 26)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	tooMany, err := json.Marshal(map[string]any{"attachments": ids})
	require.NoError(t, err)
	request(http.MethodPost, "/attachments/download-status", tooMany, 400)
	request(http.MethodPost, "/attachments/download-status", []byte(fmt.Sprintf(`{"attachments":[%q]}`, uuid.NewString())), 404)
	cross := httptest.NewRequest(http.MethodPost, "/attachments/download-status", bytes.NewReader(body))
	cross.Header.Set("Origin", "https://another.example")
	blocked := httptest.NewRecorder()
	handler.ServeHTTP(blocked, cross)
	require.Equal(t, http.StatusForbidden, blocked.Code)
	var history []models.AttachmentDownloadReport
	require.NoError(t, json.Unmarshal(request(http.MethodGet, "/attachments/"+attachment+"/download-history", nil, 200).Body.Bytes(), &history))
	require.Len(t, history, 2, "grouping does not rewrite immutable original reports")
}
