//go:build windows

package cli

import "errors"

func reexec(string) error { return errors.New("源码构建恢复后请重新启动程序") }
