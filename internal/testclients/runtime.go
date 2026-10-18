// Package testclients supports opt-in compatibility tests of external API clients.
// Production code must not import this package. The ordinary Go suite does not
// need Python or a checkout of stash-integrations.
package testclients

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Runtime enables only explicitly requested client tests. An incomplete explicit
// setup is a failure, never a successful skip. CI uses -count=1 because client
// sources live outside the Go module and its test-cache dependency boundary.
func Runtime(t *testing.T, name string) (python, source string) {
	t.Helper()
	if os.Getenv("STASH_TEST_CLIENTS") == "" {
		t.Skip("external client compatibility test; run make test-clients")
	}
	if os.Getenv("STASH_TEST_CLIENTS") != "1" {
		t.Fatal("STASH_TEST_CLIENTS must be 1 when requesting client tests")
	}
	root := os.Getenv("STASH_INTEGRATIONS_ROOT")
	if !filepath.IsAbs(root) {
		t.Fatal("STASH_INTEGRATIONS_ROOT must name an absolute stash-integrations checkout")
	}
	if name != "gallery-dl" && name != "archive" {
		t.Fatalf("unknown integration package %q", name)
	}
	source = filepath.Join(root, "integrations", name)
	if _, err := os.Stat(filepath.Join(source, "pyproject.toml")); err != nil {
		t.Fatalf("missing integration package: %v", err)
	}
	var err error
	python, err = exec.LookPath(os.Getenv("PRODUCER_PYTHON"))
	if err != nil {
		t.Fatalf("PRODUCER_PYTHON must name the prepared integration runtime: %v", err)
	}
	return python, source
}
