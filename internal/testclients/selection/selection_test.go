package selection

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectsTransitiveHelpersWithoutUnrelatedCoreTests(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "example_test.go"), []byte(`package example
 func client(t *testing.T) { testclients.Runtime(t, "archive") }
 func nested(t *testing.T) { client(t) }
 func TestRestore(t *testing.T) { t.Run("restore", func(t *testing.T) { nested(t) }) }
 func TestCore(t *testing.T) { core(t) }
 func core(t *testing.T) {}
 func cycleOne(t *testing.T) { cycleTwo(t) }
 func cycleTwo(t *testing.T) { cycleOne(t) }
 `), 0600))
	suites, err := Suites(root)
	require.NoError(t, err)
	require.Equal(t, []Suite{{Package: "./.", Tests: []string{"TestRestore"}}}, suites)
}
