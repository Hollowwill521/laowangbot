package sourceplugin

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// systemd/launchd do not inherit interactive shell profiles. Find standard Go
// installations explicitly without sourcing shell files or executing shell text.
func findGo() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("LAOWANGBOT_GO")); configured != "" {
		if !filepath.IsAbs(configured) {
			return "", errors.New("LAOWANGBOT_GO 必须是 Go 可执行文件的绝对路径")
		}
		path, e := exec.LookPath(configured)
		if e != nil {
			return "", fmt.Errorf("LAOWANGBOT_GO 指定的 Go 不可用：%w", e)
		}
		return path, nil
	}
	if path, e := exec.LookPath("go"); e == nil {
		return path, nil
	}
	candidates := []string{"/usr/local/go/bin/go", "/opt/homebrew/bin/go", "/usr/local/bin/go", "/usr/bin/go"}
	if runtime.GOOS == "windows" {
		candidates = []string{filepath.Join(os.Getenv("ProgramFiles"), "Go", "bin", "go.exe")}
	}
	for _, candidate := range candidates {
		if path, e := exec.LookPath(candidate); e == nil {
			return path, nil
		}
	}
	return "", errors.New("安装源码插件需要 Go 1.26+ 和 Git；当前服务未找到 Go。请安装 Go，或在服务环境设置 LAOWANGBOT_GO=/usr/local/go/bin/go 后重启 laowangbot（仅修改终端 PATH 不会更新 systemd 环境）")
}
