package main

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

	"github.com/spf13/pflag"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestNativeSnapshotCommandHelper(t *testing.T) {
	if os.Getenv("STASH_TEST_SNAPSHOT_COMMAND") != "1" {
		return
	}
	pflag.CommandLine = pflag.NewFlagSet("stash", pflag.ExitOnError)
	os.Args = []string{"stash", "--verify-native-snapshot", os.Getenv("STASH_TEST_SNAPSHOT_PATH")}
	main()
	os.Exit(0)
}

func TestNativeSnapshotArchiveVerificationProtocol(t *testing.T) {
	config.InitializeEmpty()
	root := t.TempDir()
	path := filepath.Join(root, "native.sqlite")
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(path))
	require.NoError(t, db.Close())
	// Execute the real main/flag path from this test binary. The shell wrapper
	// only translates its two protocol arguments into the existing child helper.
	validator := filepath.Join(root, "native validator")
	body := "#!/bin/sh\nif [ \"$#\" -ne 2 ] || [ \"$1\" != --verify-native-snapshot ]; then exit 99; fi\n" +
		"export STASH_TEST_SNAPSHOT_COMMAND=1\nexport STASH_TEST_SNAPSHOT_PATH=\"$2\"\nexec '" +
		strings.ReplaceAll(os.Args[0], "'", "'\"'\"'") + "' -test.run='^TestNativeSnapshotCommandHelper$'\n"
	require.NoError(t, os.WriteFile(validator, []byte(body), 0700))
	python := os.Getenv("PRODUCER_PYTHON")
	if python == "" {
		python = "python3"
	}
	archive, err := filepath.Abs(filepath.Join("..", "..", "integrations", "archive"))
	require.NoError(t, err)
	entries, err := os.ReadDir(filepath.Join(archive, "src", "stash_archive"))
	require.NoError(t, err)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".py") {
			_, err := os.ReadFile(filepath.Join(archive, "src", "stash_archive", entry.Name()))
			require.NoError(t, err) // Include Python source in the Go test cache.
		}
	}
	script := filepath.Join(archive, "tests", "native_command_check.py")
	_, err = os.ReadFile(script)
	require.NoError(t, err)
	input, err := json.Marshal(map[string]string{"database": path, "validator": validator})
	require.NoError(t, err)
	configuration := filepath.Join(root, "unused-config.yml")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, script)
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(archive, "src"),
		"STASH_CONFIG_FILE="+configuration)
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	var result struct {
		Verified bool `json:"verified"`
	}
	require.NoError(t, json.Unmarshal(output, &result))
	require.True(t, result.Verified)
	require.NoFileExists(t, configuration)
}

func TestNativeSnapshotCommandExitsWithoutApplicationInitialization(t *testing.T) {
	config.InitializeEmpty()
	root := t.TempDir()
	path := filepath.Join(root, "snapshot.sqlite")
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(path))
	require.NoError(t, db.Close())
	for _, tc := range []struct {
		name, input string
		valid       bool
	}{
		{"native snapshot", path, true},
		{"missing file", filepath.Join(root, "missing.sqlite"), false},
		{"empty flag", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configuration := filepath.Join(t.TempDir(), "unused-config.yml")
			command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestNativeSnapshotCommandHelper$")
			command.Env = append(os.Environ(), "STASH_TEST_SNAPSHOT_COMMAND=1", "STASH_TEST_SNAPSHOT_PATH="+tc.input,
				"STASH_CONFIG_FILE="+configuration)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			err := command.Run()
			if tc.valid {
				require.NoError(t, err, stderr.String())
				var report sqlite.NativeSnapshotReport
				require.NoError(t, json.Unmarshal(stdout.Bytes(), &report))
				require.True(t, report.DatabaseVerified)
				require.False(t, report.FilesystemRecoveryVerified)
				require.Equal(t, sqlite.GetRequiredSchemaVersion(), report.SchemaVersion)
			} else {
				var exited *exec.ExitError
				require.ErrorAs(t, err, &exited)
				require.Equal(t, 1, exited.ExitCode())
				require.Empty(t, stdout.String())
				require.NotEmpty(t, stderr.String())
			}
			require.NoFileExists(t, configuration)
		})
	}
}
