//go:build !linux

package fsutil

import "os"

// Other platforms retain their existing identity format. Windows already uses
// the volume serial and file index. Unix device-number bindings remain strict;
// they are not silently promoted without a supported filesystem identity.
func OpenPersistentFileIdentity(file *os.File) (string, error) {
	return OpenFileIdentity(file)
}
