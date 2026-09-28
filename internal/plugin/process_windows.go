package plugin

import "os/exec"

// Windows CommandContext terminates the direct process. Plugins own child cleanup.
func configureProcess(cmd *exec.Cmd) {}
