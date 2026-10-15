package cpulimit

import (
	"strconv"
	"strings"
)

// procStat is what the limiter reads from /proc/<pid>/stat.
type procStat struct {
	pid, ppid int
	ticks     uint64 // user + system CPU time, in clock ticks
}

// parseStat parses a /proc/<pid>/stat line. The command name sits in
// parentheses and may hold spaces and parentheses itself, so the fields
// are counted from the last ")".
func parseStat(line string) (procStat, bool) {
	open := strings.IndexByte(line, '(')
	end := strings.LastIndexByte(line, ')')
	if open < 1 || end < open {
		return procStat{}, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line[:open]))
	if err != nil {
		return procStat{}, false
	}
	// After the name: state(3) ppid(4) … utime(14) stime(15).
	f := strings.Fields(line[end+1:])
	if len(f) < 13 {
		return procStat{}, false
	}
	ppid, err1 := strconv.Atoi(f[1])
	utime, err2 := strconv.ParseUint(f[11], 10, 64)
	stime, err3 := strconv.ParseUint(f[12], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return procStat{}, false
	}
	return procStat{pid: pid, ppid: ppid, ticks: utime + stime}, true
}

// descendants returns the processes below root, by walking parent links.
func descendants(all []procStat, root int) []procStat {
	children := map[int][]procStat{}
	for _, p := range all {
		children[p.ppid] = append(children[p.ppid], p)
	}
	var out []procStat
	queue := []int{root}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		for _, c := range children[next] {
			if c.pid == root {
				continue
			}
			out = append(out, c)
			queue = append(queue, c.pid)
		}
	}
	return out
}
