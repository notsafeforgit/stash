// check_action_pins rejects mutable action references in workflows and local actions.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

var (
	commitRef = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(?:/[^@\s]+)?@[a-fA-F0-9]{40}$`)
	digestRef = regexp.MustCompile(`^docker://[^@\s]+@sha256:[a-fA-F0-9]{64}$`)
)

type step struct {
	Uses string `yaml:"uses"`
}

type job struct {
	Uses  string `yaml:"uses"`
	Steps []step `yaml:"steps"`
}

type definition struct {
	Jobs map[string]job `yaml:"jobs"`
	Runs struct {
		Steps []step `yaml:"steps"`
		Image string `yaml:"image"`
	} `yaml:"runs"`
}

func pinned(ref string) bool {
	if strings.Contains(ref, "${{") {
		return false
	}
	// Local actions/workflows are versioned with the calling repository.
	return strings.HasPrefix(ref, "./") || commitRef.MatchString(ref) || digestRef.MatchString(ref)
}

func checkDefinition(data []byte) ([]string, error) {
	var doc definition
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&doc); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("expected a single YAML document")
	}

	var problems []string
	check := func(location, ref string) {
		if ref != "" && !pinned(ref) {
			problems = append(problems, fmt.Sprintf("%s: %q must use a full 40-character commit SHA (or a SHA-256 digest for docker:// actions)", location, ref))
		}
	}
	for name, job := range doc.Jobs {
		check("jobs."+name+".uses", job.Uses)
		for i, step := range job.Steps {
			check(fmt.Sprintf("jobs.%s.steps[%d].uses", name, i), step.Uses)
		}
	}
	for i, step := range doc.Runs.Steps {
		check(fmt.Sprintf("runs.steps[%d].uses", i), step.Uses)
	}
	if strings.HasPrefix(doc.Runs.Image, "docker://") {
		check("runs.image", doc.Runs.Image)
	}
	sort.Strings(problems)
	return problems, nil
}

func isDefinition(path string) bool {
	base := filepath.Base(path)
	if base == "action.yml" || base == "action.yaml" {
		return true
	}
	ext := filepath.Ext(path)
	return strings.HasPrefix(filepath.ToSlash(path), ".github/workflows/") && (ext == ".yml" || ext == ".yaml")
}

func checkRepository(root string) error {
	// Include new files before commit, but not ignored dependency checkouts.
	command := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	command.Dir = root
	paths, err := command.Output()
	if err != nil {
		return fmt.Errorf("list repository files: %w", err)
	}
	var problems []error
	checked := 0
	for _, path := range strings.Split(string(paths), "\x00") {
		if !isDefinition(path) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, path))
		if errors.Is(err, os.ErrNotExist) {
			continue // A tracked file may have been deleted in the working tree.
		}
		if err != nil {
			return err
		}
		checked++
		violations, err := checkDefinition(data)
		if err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", path, err))
		}
		for _, violation := range violations {
			problems = append(problems, fmt.Errorf("%s: %s", path, violation))
		}
	}
	if checked == 0 {
		return errors.New("no workflow or action definitions found; run from the repository root")
	}
	return errors.Join(problems...)
}

func main() {
	if err := checkRepository("."); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("All external workflow and action references are pinned.")
}
