// Run the opt-in Go/client compatibility suite, with no cached external inputs.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/stashapp/stash/internal/testclients/selection"
)

func main() {
	tags := flag.String("tags", "integration sqlite_stat4 sqlite_math_functions sqlite_fts5", "Go build tags")
	timeout := flag.String("timeout", "20m", "Go package timeout")
	list := flag.Bool("list", false, "print selected tests without executing")
	flag.Parse()
	suites, err := selection.Suites(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(suites) == 0 {
		fmt.Fprintln(os.Stderr, "No client contract tests found")
		os.Exit(1)
	}
	var packages, names []string
	for _, suite := range suites {
		packages = append(packages, suite.Package)
		names = append(names, suite.Tests...)
		if *list {
			fmt.Printf("%s: %s\n", suite.Package, strings.Join(suite.Tests, ", "))
		}
	}
	if *list {
		return
	}
	if os.Getenv("STASH_TEST_CLIENTS") != "1" || os.Getenv("STASH_INTEGRATIONS_ROOT") == "" || os.Getenv("PRODUCER_PYTHON") == "" {
		fmt.Fprintln(os.Stderr, "Use make test-clients with the prepared integration checkout/runtime")
		os.Exit(1)
	}
	args := []string{"test", "-count=1", "-timeout", *timeout, "-tags", *tags, "-run", "^(" + strings.Join(names, "|") + ")$"}
	command := exec.Command("go", append(args, packages...)...)
	command.Stdout, command.Stderr, command.Stdin = os.Stdout, os.Stderr, os.Stdin
	if err := command.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
