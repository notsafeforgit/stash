package sourceadapter_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Site interpretation must stay offline and must not acquire a repository or
// runtime dependency as new extractors are added. Core integration tests cover
// persistence through pkg/archive; this guards the other side of that boundary.
func TestAdapterDependenciesStayOffline(t *testing.T) {
	allowed := map[string]bool{}
	for _, name := range strings.Fields(`
		crypto/sha256 encoding/hex encoding/json errors fmt html math math/big
		net/url path regexp slices strconv strings time unicode unicode/utf8
		github.com/stashapp/stash/pkg/jsonutil
		github.com/stashapp/stash/pkg/models
		golang.org/x/text/cases golang.org/x/text/unicode/norm
	`) {
		allowed[name] = true
	}
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, spec := range file.Imports {
			dependency, err := strconv.Unquote(spec.Path.Value)
			require.NoError(t, err)
			require.True(t, allowed[dependency], "%s imports %s: adapters may only interpret supplied evidence", name, dependency)
		}
	}
}
