package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

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
