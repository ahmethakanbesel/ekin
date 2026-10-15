//go:build darwin

package cpulimit

import "golang.org/x/sys/unix"

// NiceSupported reports whether this OS can lower job processes' priority.
func NiceSupported() bool { return true }

// jobProcs returns the processes below root (CPU time is not read).
func jobProcs(root int) []procStat {
	all, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil
	}
	out := make([]procStat, 0, len(all))
	for _, p := range all {
		out = append(out, procStat{pid: int(p.Proc.P_pid), ppid: int(p.Eproc.Ppid)})
	}
	return descendants(out, root)
}

// renice sets pid to nice n; on macOS the priority is per process.
func renice(pid, n int) {
	_ = unix.Setpriority(unix.PRIO_PROCESS, pid, n) // fails only for a higher priority or a gone process
}
