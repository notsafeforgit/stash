package fsutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpgradedFileIdentityRequiresTheReviewedFilesystem(t *testing.T) {
	root := "fsid-v1:2fc12fc1:15e26930001d8459:780"
	file := "fsid-v1:2fc12fc1:15e26930001d8459:1234"
	require.True(t, ValidFileIdentityUpgrade("2c:780", root))
	id, err := upgradedFileIdentity(file, "2c:780", root)
	require.NoError(t, err)
	require.Equal(t, "2c:1234", id)
	for _, legacy := range []string{"2c:781", "2c:", "bogus:780", "2C:780", root} {
		require.False(t, ValidFileIdentityUpgrade(legacy, root))
		_, err := upgradedFileIdentity(file, legacy, root)
		require.Error(t, err)
	}
	for _, other := range []string{
		"fsid-v1:2fc12fc1:25e26930001d8459:1234", // same inode on another filesystem
		"fsid-v1:ef53:15e26930001d8459:1234",     // a different filesystem type
		"fsid-v1:2fc12fc1:0000000000000000:1234", "2d:1234", "fsid-v1:garbage:x:y",
	} {
		_, err := upgradedFileIdentity(other, "2c:780", root)
		require.Error(t, err, other)
	}
}
