package platform

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"
)

const RestartExitCode = 75

// Supervise restarts only deliberate restart requests; failures remain visible to the OS service manager.
func Supervise(ctx context.Context, binary string, args []string, in io.Reader, out, errOut io.Writer) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		child := exec.CommandContext(ctx, binary, args...)
		child.Env = append(os.Environ(), "LAOWANGBOT_SUPERVISED=1")
		child.Stdin = in
		child.Stdout = out
		child.Stderr = errOut
		child.Cancel = func() error {
			e := child.Process.Signal(os.Interrupt)
			if e != nil {
				return child.Process.Kill()
			}
			return nil
		}
		child.WaitDelay = 5 * time.Second
		err := child.Run()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != RestartExitCode {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}
