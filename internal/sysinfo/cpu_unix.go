//go:build !windows

package sysinfo

import (
	"syscall"
	"time"
)

// processCPU 是本进程至今用掉的 CPU 时间（用户态加内核态）。
func processCPU() time.Duration {
	var usage syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &usage) != nil {
		return 0
	}
	return time.Duration(usage.Utime.Nano() + usage.Stime.Nano())
}
