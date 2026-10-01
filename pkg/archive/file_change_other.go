//go:build !linux

package archive

import "io/fs"

// Platforms without a change-counter adapter rehash during revalidation to
// detect same-size writes followed by restoring the old modification time.
func fileChangeToken(info fs.FileInfo) string { return "" }
