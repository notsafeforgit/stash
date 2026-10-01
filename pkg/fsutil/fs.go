// Package fsutil provides filesystem utility functions for the application.
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"unicode"
)

// IsFsPathCaseSensitive checks the fs of the given path to see if it is case sensitive
// if the case sensitivity can not be determined false and an error != nil are returned
func IsFsPathCaseSensitive(path string) (bool, error) {
	// The case sensitivity of the fs of "path" is determined by case flipping
	// the first letter rune from the base string of the path
	// If the resulting flipped path exists then the fs should not be case sensitive
	// File identity distinguishes a spelling alias from two distinct files.

	fi, err := os.Stat(path)
	if err != nil { // path cannot be stat'd
		return false, err
	}

	base := filepath.Base(path)
	fBase, err := flipCaseSingle(base)
	if err != nil { // cannot be case flipped
		return false, err
	}

	flippedPath := filepath.Join(filepath.Dir(path), fBase)

	fiCase, err := os.Stat(flippedPath)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return !os.SameFile(fi, fiCase), nil
}

// flipCaseSingle flips the case ( lower<->upper ) of a single char from the string s
// If the string cannot be flipped, the original string value and an error are returned
func flipCaseSingle(s string) (string, error) {
	rr := []rune(s)
	for i, r := range rr {
		flipped := unicode.ToUpper(r)
		if unicode.IsUpper(r) {
			flipped = unicode.ToLower(r)
		}
		if flipped != r {
			rr[i] = flipped
			return string(rr), nil
		}
	}
	return s, fmt.Errorf("could not case flip string %s", s)
}
