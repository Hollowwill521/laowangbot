package sourceplugin

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Only expose package names and dependency download notices, never arbitrary
// subprocess output (which can contain local paths or credentials).
var buildActivity = regexp.MustCompile(`^(?:[A-Za-z0-9._/-]+|go: downloading [A-Za-z0-9._/-]+ v[A-Za-z0-9.+_-]+)$`)

type buildOutput struct {
	bounded
	line     []byte
	long     bool
	progress func(string)
}

func (w *buildOutput) Write(p []byte) (int, error) {
	w.bounded.Write(p)
	for _, c := range p {
		if c == '\n' {
			line := strings.TrimSpace(string(w.line))
			if !w.long && w.progress != nil && buildActivity.MatchString(line) {
				w.progress("编译 / 下载依赖：" + line)
			}
			w.line = w.line[:0]
			w.long = false
		} else if len(w.line) < 512 {
			w.line = append(w.line, c)
		} else {
			w.long = true
		}
	}
	return len(p), nil
}

func runBuild(cmd *exec.Cmd, progress func(string)) error {
	output := &buildOutput{progress: progress}
	// os/exec serializes writes when stdout and stderr share the same writer.
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w\n%s", filepath.Base(cmd.Path), err, output.data)
	}
	return nil
}
