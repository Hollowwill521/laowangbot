//go:build !windows

package cli

import (
	"os"
	"syscall"
)

// A recovered executable must be loaded again; the current image may be the
// interrupted candidate with a different static plugin registry.
func reexec(binary string) error {
	return syscall.Exec(binary, append([]string{binary}, os.Args[1:]...), os.Environ())
}
