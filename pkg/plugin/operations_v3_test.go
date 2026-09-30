package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/session"
	"github.com/stretchr/testify/require"
)

func TestDeclaredOperationsV3(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "fixture.yml")
	require.NoError(t, os.WriteFile(manifest, []byte(`apiVersion: 3
name: Operations fixture
interface: js
exec: [operation.js]
operations:
  review: {kind: QUERY}
  apply: {kind: MUTATION, description: Apply reviewed changes}
`), 0600))
	cfg, err := loadPluginFromYAMLFile(manifest)
	require.NoError(t, err)
	c := NewCache(previewServerConfig{})
	c.plugins = []Config{*cfg}
	c.RegisterSessionStore(session.NewStore(previewSessionConfig{}))
	script := filepath.Join(dir, "operation.js")
	require.NoError(t, os.WriteFile(script, []byte(`({Output: input.Args})`), 0600))
	result, err := c.OperationV3(context.Background(), "fixture", "review", PluginOperationQueryV3, map[string]interface{}{"mode": "apply", "ids": []string{"1", "2"}})
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.JSONEq(t, `{"mode":"operation","operation":"review","operation_type":"query","input":{"mode":"apply","ids":["1","2"]}}`, string(encoded))
	_, err = c.OperationV3(context.Background(), "fixture", "apply", PluginOperationMutationV3, nil)
	require.NoError(t, err)
	for _, name := range []string{"missing", "apply"} {
		_, err = c.OperationV3(context.Background(), "fixture", name, PluginOperationQueryV3, nil)
		require.ErrorContains(t, err, "no QUERY operation")
	}
	_, err = c.OperationV3(context.Background(), "fixture", "review", PluginOperationMutationV3, nil)
	require.ErrorContains(t, err, "no MUTATION operation")
	_, err = c.OperationV3(context.Background(), "missing", "review", PluginOperationQueryV3, nil)
	require.ErrorContains(t, err, "not found")
	c.config = previewServerConfig{disabled: []string{"fixture"}}
	_, err = c.OperationV3(context.Background(), "fixture", "review", PluginOperationQueryV3, nil)
	require.ErrorContains(t, err, "disabled")
	c.config = previewServerConfig{}
	_, err = c.OperationV3(context.Background(), "fixture", "review", PluginOperationQueryV3, map[string]interface{}{"huge": strings.Repeat("x", operationMaxInputV3)})
	require.ErrorContains(t, err, "input must be JSON")
	require.NoError(t, os.WriteFile(script, []byte(`({Output: "x".repeat(4194305)})`), 0600))
	_, err = c.OperationV3(context.Background(), "fixture", "review", PluginOperationQueryV3, nil)
	require.ErrorContains(t, err, "output must be JSON")
	require.NoError(t, os.WriteFile(script, []byte(`({Error: "Catalog unavailable"})`), 0600))
	_, err = c.OperationV3(context.Background(), "fixture", "review", PluginOperationQueryV3, nil)
	require.ErrorContains(t, err, "Catalog unavailable")
	require.NoError(t, os.WriteFile(script, []byte(`while (true) {}`), 0600))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = c.OperationV3(ctx, "fixture", "review", PluginOperationQueryV3, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestOperationsManifestV3(t *testing.T) {
	for _, definition := range []string{
		"review: {kind: UNKNOWN}", "review: {}", "'': {kind: QUERY}",
		"review: {kind: QUERY, args: {mode: sync}}", "../bad: {kind: QUERY}",
	} {
		_, err := loadPluginFromYAML(strings.NewReader("apiVersion: 3\nname: Invalid\noperations:\n  " + definition))
		require.Error(t, err, definition)
	}
}
