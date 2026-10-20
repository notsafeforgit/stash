package covermigration_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCoverManifestReaderIsUpgradeOnly(t *testing.T) {
	root := filepath.Join("..", "..")
	require.FileExists(t, filepath.Join(root, "go.mod"))
	err := fs.WalkDir(os.DirFS(root), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name != "." && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor" || entry.Name() == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, filepath.FromSlash(name)), nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			dependency, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if dependency == "github.com/stashapp/stash/pkg/covermigration" {
				require.Equal(t, "internal/manager/scene_cover_upgrade.go", name,
					"runtime cover queries and generation must use SQLite authored state")
			}
		}
		return nil
	})
	require.NoError(t, err)
}
