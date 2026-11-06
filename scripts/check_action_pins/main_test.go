package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testSHA = "0123456789abcdef0123456789abcdef01234567"

func TestPinned(t *testing.T) {
	for _, ref := range []string{
		"actions/checkout@" + testSHA,
		"owner/repo/sub/action@" + testSHA,
		"owner/repo/.github/workflows/build.yml@" + testSHA,
		"./.github/actions/setup",
		"./.github/workflows/native-ci.yml",
		"docker://ghcr.io/owner/image@sha256:" + strings.Repeat("a", 64),
	} {
		if !pinned(ref) {
			t.Errorf("rejected pinned or local reference %q", ref)
		}
	}
	for _, ref := range []string{
		"actions/checkout@v6", "actions/checkout@v6.0.0", "owner/repo@main",
		"owner/repo@latest", "owner/repo@0123456", "owner/repo",
		"owner/repo@" + testSHA + "a", "owner/repo@" + strings.Repeat("g", 40),
		"owner/repo@${{ inputs.ref }}", "./${{ inputs.action }}",
		"owner/repo/.github/workflows/build.yml@main",
		"docker://alpine:3", "docker://alpine@sha256:1234",
		"docker://alpine@" + testSHA,
	} {
		if pinned(ref) {
			t.Errorf("accepted mutable or invalid reference %q", ref)
		}
	}
}

func TestCheckDefinition(t *testing.T) {
	for _, test := range []struct {
		name, yaml string
		violations int
	}{
		{"quoted SHA and comments", fmt.Sprintf("jobs:\n  test:\n    steps:\n      - uses: 'actions/checkout@%s' # v6\n", testSHA), 0},
		{"inline mapping", "jobs: {test: {steps: [{uses: 'actions/checkout@v6'}]}}", 1},
		{"quoted key", "jobs: {test: {steps: [{'uses': 'actions/checkout@v6'}]}}", 1},
		{"folded scalar", "jobs:\n  test:\n    steps:\n      - uses: >-\n          actions/checkout@v6\n", 1},
		{"reusable workflow", "jobs: {test: {uses: 'owner/repo/.github/workflows/test.yml@main'}}", 1},
		{"composite action", "runs: {using: composite, steps: [{uses: 'actions/cache@v5'}]}", 1},
		{"Docker action", "runs: {using: docker, image: 'docker://alpine:3'}", 1},
		{"local Dockerfile", "runs: {using: docker, image: Dockerfile}", 0},
		{"aliases and merges", "jobs:\n  test:\n    steps:\n      - &setup {uses: 'actions/checkout@v6'}\n      - *setup\n      - <<: *setup\n        name: Merged step\n", 3},
		{"unrelated uses text", "jobs:\n  test:\n    steps:\n      - run: |\n          echo 'uses: owner/repo@main'\n        env: {uses: example}\n", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			problems, err := checkDefinition([]byte(test.yaml))
			if err != nil {
				t.Fatal(err)
			}
			if len(problems) != test.violations {
				t.Fatalf("got %v; want %d violations", problems, test.violations)
			}
		})
	}
}

func TestRejectMalformedYAML(t *testing.T) {
	for _, data := range []string{
		"", "jobs: [", "jobs: {test: {steps: [{uses: {bad: value}}]}}",
		"jobs: {}\n---\njobs: {test: {uses: 'owner/repo@main'}}",
		"jobs: {}\njobs: {}", "jobs: {test: {steps: [*missing]}}",
	} {
		if _, err := checkDefinition([]byte(data)); err == nil {
			t.Errorf("accepted invalid YAML: %s", data)
		}
	}
}

func TestCheckRepository(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", output, err)
	}
	write := func(path, data string) {
		t.Helper()
		path = filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "ignored/\n")
	badAction := "runs: {using: composite, steps: [{uses: 'owner/repo@main'}]}"
	write("ignored/action.yml", badAction)
	write(".github/ISSUE_TEMPLATE/example.yml", "this is not a workflow")
	if err := checkRepository(root); err == nil {
		t.Fatal("expected failure when there are no workflow or action definitions")
	}
	write(".github/workflows/check.yml", "jobs: {test: {uses: './.github/workflows/reuse.yml'}}")
	write(".github/workflows/reuse.yaml", fmt.Sprintf("jobs: {test: {steps: [{uses: 'actions/checkout@%s'}]}}", testSHA))
	if err := checkRepository(root); err != nil {
		t.Fatal(err)
	}
	// Discover new composite actions anywhere in the repository, in either extension.
	for _, path := range []string{".github/actions/test/action.yml", "custom/test/action.yaml"} {
		write(path, badAction)
	}
	err := checkRepository(root)
	if err == nil {
		t.Fatal("expected unpinned composite actions to fail")
	}
	for _, path := range []string{".github/actions/test/action.yml", "custom/test/action.yaml"} {
		if !strings.Contains(err.Error(), path) {
			t.Errorf("missing diagnostic for %s: %v", path, err)
		}
	}
}
