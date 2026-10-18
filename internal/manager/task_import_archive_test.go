package manager

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeImportArchive(t *testing.T, names ...string) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "import-*.zip")
	require.NoError(t, err)
	w := zip.NewWriter(file)
	for _, name := range names {
		entry, err := w.Create(name)
		require.NoError(t, err)
		if name[len(name)-1] != '/' {
			_, err = entry.Write([]byte(`{"name":"historical fixture"}`))
			require.NoError(t, err)
		}
	}
	require.NoError(t, w.Close())
	require.NoError(t, file.Close())
	return file.Name()
}

func TestImportArchivePaths(t *testing.T) {
	for _, name := range []string{"../escape.json", "nested/../../escape.json", "/escape.json", `..\escape.json`, `C:/escape.json`, "./tags/tag.json", "nested/../tag.json"} {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			task := ImportTask{BaseDir: base, TmpZip: writeImportArchive(t, "tags/valid.json", name)}
			require.Error(t, task.unzipFile())
			entries, err := os.ReadDir(base)
			require.NoError(t, err)
			require.Empty(t, entries, "reject the whole archive before writing even its valid entries")
		})
	}
}

func TestImportArchiveHistoricalLayout(t *testing.T) {
	base := t.TempDir()
	task := ImportTask{BaseDir: base, TmpZip: writeImportArchive(t,
		"tags/", "tags/abc123.json", "scenes/def456.json", "performers/日本語.json", "files/nested/file.json")}
	require.NoError(t, task.unzipFile())
	for _, name := range []string{"tags/abc123.json", "scenes/def456.json", "performers/日本語.json", "files/nested/file.json"} {
		data, err := os.ReadFile(filepath.Join(base, filepath.FromSlash(name)))
		require.NoError(t, err)
		require.JSONEq(t, `{"name":"historical fixture"}`, string(data))
	}
	require.NoFileExists(t, task.TmpZip)
}

func TestImportArchiveRejectsSymlinkEscape(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{true: "directory", false: "file"}[directory], func(t *testing.T) {
			base, outside := t.TempDir(), t.TempDir()
			target := filepath.Join(outside, "retained.json")
			require.NoError(t, os.WriteFile(target, []byte("retain me"), 0600))
			link, name := target, "entry.json"
			if directory {
				link, name = outside, "entry.json/retained.json"
			}
			if err := os.Symlink(link, filepath.Join(base, "entry.json")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			task := ImportTask{BaseDir: base, TmpZip: writeImportArchive(t, name)}
			require.Error(t, task.unzipFile())
			data, err := os.ReadFile(target)
			require.NoError(t, err)
			require.Equal(t, "retain me", string(data))
		})
	}
}

func TestImportArchiveRejectsSpecialFiles(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "import-*.zip")
	require.NoError(t, err)
	w := zip.NewWriter(file)
	header := &zip.FileHeader{Name: "link.json"}
	header.SetMode(os.ModeSymlink | 0777)
	_, err = w.CreateHeader(header)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, file.Close())
	task := ImportTask{BaseDir: t.TempDir(), TmpZip: file.Name()}
	require.ErrorContains(t, task.unzipFile(), "unsafe import archive entry")
}
