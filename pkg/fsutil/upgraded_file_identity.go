package fsutil

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

func identityHex(value string) bool {
	n, err := strconv.ParseUint(value, 16, 64)
	return err == nil && strconv.FormatUint(n, 16) == value
}

func persistentIdentityParts(identity string) (filesystem, inode string, ok bool) {
	parts := strings.Split(identity, ":")
	if len(parts) != 4 || parts[0]+":" != PersistentFileIdentityPrefix || !identityHex(parts[1]) ||
		len(parts[2]) != 16 || parts[2] == "0000000000000000" || !identityHex(parts[3]) {
		return "", "", false
	}
	if _, err := strconv.ParseUint(parts[2], 16, 64); err != nil || strings.ToLower(parts[2]) != parts[2] {
		return "", "", false
	}
	return strings.Join(parts[:3], ":"), parts[3], true
}

// ValidFileIdentityUpgrade only establishes matching inode syntax. The caller
// must explicitly review the filesystem and verify the open root descriptor.
func ValidFileIdentityUpgrade(legacy, persistent string) bool {
	parts := strings.Split(legacy, ":")
	_, inode, ok := persistentIdentityParts(persistent)
	return ok && len(parts) == 2 && identityHex(parts[0]) && identityHex(parts[1]) && parts[1] == inode
}

// OpenUpgradedFileIdentity retains legacy snapshot keys after a reviewed
// identity upgrade, but only on the exact filesystem pinned by that upgrade.
// A mount with colliding inode numbers cannot acquire the old device namespace.
func OpenUpgradedFileIdentity(file *os.File, legacyRoot, persistentRoot string) (string, error) {
	identity, err := OpenPersistentFileIdentity(file)
	if err != nil {
		return "", err
	}
	return upgradedFileIdentity(identity, legacyRoot, persistentRoot)
}

func upgradedFileIdentity(identity, legacyRoot, persistentRoot string) (string, error) {
	filesystem, inode, ok := persistentIdentityParts(identity)
	rootFilesystem, _, rootOK := persistentIdentityParts(persistentRoot)
	if !ok || !rootOK || filesystem != rootFilesystem || !ValidFileIdentityUpgrade(legacyRoot, persistentRoot) {
		return "", errors.New("file is outside the filesystem reviewed for the legacy identity upgrade")
	}
	device, _, _ := strings.Cut(legacyRoot, ":")
	return device + ":" + inode, nil
}
