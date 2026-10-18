// Package selection finds real-server tests that call the external client runtime.
package selection

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

type Suite struct {
	Package string
	Tests   []string
}

// Suites follows local test-helper calls, including calls inside subtests. This
// keeps the compatibility job focused without a hand-maintained test-name list.
func Suites(root string) ([]Suite, error) {
	directories := map[string]map[string]map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		directory := filepath.Dir(path)
		if directories[directory] == nil {
			directories[directory] = map[string]map[string]bool{}
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			calls := map[string]bool{}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch target := call.Fun.(type) {
				case *ast.Ident:
					calls[target.Name] = true
				case *ast.SelectorExpr:
					if receiver, ok := target.X.(*ast.Ident); ok && receiver.Name == "testclients" && target.Sel.Name == "Runtime" {
						calls["@client"] = true
					}
				}
				return true
			})
			directories[directory][function.Name.Name] = calls
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var suites []Suite
	for directory, functions := range directories {
		selected := map[string]bool{"@client": true}
		for changed := true; changed; {
			changed = false
			for name, calls := range functions {
				if selected[name] {
					continue
				}
				for call := range calls {
					if selected[call] {
						selected[name] = true
						changed = true
						break
					}
				}
			}
		}
		var tests []string
		for name := range functions {
			if strings.HasPrefix(name, "Test") && selected[name] {
				tests = append(tests, name)
			}
		}
		if len(tests) == 0 {
			continue
		}
		sort.Strings(tests)
		relative, err := filepath.Rel(root, directory)
		if err != nil {
			return nil, err
		}
		suites = append(suites, Suite{Package: "./" + filepath.ToSlash(relative), Tests: tests})
	}
	sort.Slice(suites, func(i, j int) bool { return suites[i].Package < suites[j].Package })
	return suites, nil
}
