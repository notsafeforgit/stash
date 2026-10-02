//go:build windows

package translation

import "os/exec"

// translate-shell is a Unix dependency. Retained translation inspection and
// migration remain portable; enabling this provider requires a supported host.
func providerPlatformSupported() bool { return false }

func prepareProviderCommand(_ *exec.Cmd) error { return ErrProviderInput }
