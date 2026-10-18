package testclients

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuntimeChild(t *testing.T) {
	if os.Getenv("STASH_TEST_RUNTIME_CHILD") == "" {
		return
	}
	Runtime(t, "archive")
}

func TestExplicitClientChecksCannotSilentlySkipMissingDependencies(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "integrations/archive"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "integrations/archive/pyproject.toml"), nil, 0600))
	python, err := os.Executable() // Only path resolution is exercised here.
	require.NoError(t, err)
	for _, tc := range []struct {
		name, enabled, root, python string
		fail, skip                  bool
	}{
		{"ordinary Go suite", "", "", "", false, true},
		{"invalid opt-in", "yes", root, python, true, false},
		{"missing checkout", "1", filepath.Join(root, "missing"), python, true, false},
		{"relative checkout", "1", "relative", python, true, false},
		{"missing runtime", "1", root, filepath.Join(root, "missing-python"), true, false},
		{"explicit prepared checkout", "1", root, python, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := exec.Command(python, "-test.run=^TestRuntimeChild$", "-test.v")
			for _, variable := range os.Environ() {
				if !strings.HasPrefix(variable, "STASH_TEST_") && !strings.HasPrefix(variable, "STASH_INTEGRATIONS_ROOT=") && !strings.HasPrefix(variable, "PRODUCER_PYTHON=") {
					command.Env = append(command.Env, variable)
				}
			}
			command.Env = append(command.Env, "STASH_TEST_RUNTIME_CHILD=1", "STASH_TEST_CLIENTS="+tc.enabled,
				"STASH_INTEGRATIONS_ROOT="+tc.root, "PRODUCER_PYTHON="+tc.python)
			output, err := command.CombinedOutput()
			require.Equal(t, tc.fail, err != nil, string(output))
			require.Equal(t, tc.skip, strings.Contains(string(output), "--- SKIP:"), string(output))
		})
	}
}
