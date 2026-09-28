package sysinfo

import (
	"golang.org/x/sys/windows"
	"time"
)

func processCPU() time.Duration {
	var c, e, k, u windows.Filetime
	if windows.GetProcessTimes(windows.CurrentProcess(), &c, &e, &k, &u) != nil {
		return 0
	}
	ticks := uint64(k.HighDateTime)<<32 | uint64(k.LowDateTime)
	ticks += uint64(u.HighDateTime)<<32 | uint64(u.LowDateTime)
	return time.Duration(ticks * 100)
}
