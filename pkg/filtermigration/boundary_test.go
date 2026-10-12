package filtermigration_test

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

// Historical conversion must not become a fallback for a new API, import path,
// query evaluator, or UI codec. Tests may use it to build historical fixtures.
func TestLegacyConversionCallersAreUpgradeOnly(t *testing.T) {
	allowed := map[string]bool{
		"internal/manager/default_filter_migration.go":      true,
		"pkg/sqlite/migrations/1000001_saved_filter_ast.go": true,
		"pkg/sqlite/migrations/fork_compatibility.go":       true,
	}
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
			if dependency == "github.com/stashapp/stash/pkg/filtermigration" {
				require.True(t, allowed[name], "%s imports legacy conversion outside an approved upgrade entry point; use native FilterAST contracts (see pkg/filtermigration/doc.go)", name)
			}
		}
		return nil
	})
	require.NoError(t, err)
}
