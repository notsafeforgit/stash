//go:build windows

package exec

import "os/exec"

// CommandContext terminates the child on Windows. RunBounded's WaitDelay also
// closes inherited output pipes so a descendant cannot keep the caller blocked.
func prepareBoundedCommand(cmd *exec.Cmd) {}
