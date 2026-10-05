//go:build !linux

package file

import "os"

// The caller must exclude external writers as well as native deletion writers.
// Other platforms still compare identity, size, mode and modification time.
func deletionSnapshotChangeToken(info os.FileInfo) string { return "" }
