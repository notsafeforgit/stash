package api

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Check each real HTTP/journal transition, including saved lost responses, with
// the standalone verifier. The test server is idle while the readers are open.
func verifyNativeArchiveJournals(t *testing.T, database, directory, origin string) {
	t.Helper()
	python, producerPath := nativeProducerRuntime(t)
	archivePath := filepath.Join(filepath.Dir(producerPath), "archive")
	sourcePath := filepath.Join(archivePath, "src", "stash_archive")
	entries, err := os.ReadDir(sourcePath)
	require.NoError(t, err)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".py") {
			_, err := os.ReadFile(filepath.Join(sourcePath, entry.Name()))
			require.NoError(t, err) // Include interpreter inputs in Go's test cache.
		}
	}
	script := filepath.Join(archivePath, "tests", "http_receipt_check.py")
	_, err = os.ReadFile(script)
	require.NoError(t, err)
	body, err := json.Marshal(map[string]string{"library": database, "outbox": filepath.Join(directory, "outbox.sqlite"), "origin": origin})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, script)
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(archivePath, "src"))
	command.Stdin = bytes.NewReader(body)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	var result struct {
		Verified  bool `json:"verified"`
		Producers int  `json:"producers"`
	}
	require.NoError(t, json.Unmarshal(output, &result))
	require.True(t, result.Verified)
	require.Equal(t, 1, result.Producers)
}
