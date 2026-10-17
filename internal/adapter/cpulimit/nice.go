package cpulimit

import (
	"context"
	"log/slog"
	"os"
	"time"
)

// Nicer lowers the scheduling priority of the job processes to the host
// profile's nice value, so the host's own work (an editor, a build, a
// call) comes first while jobs use whatever CPU is left. The Ekin
// process keeps its priority, so the web UI stays quick. A process is
// reniced within a period of starting; raising the priority again (a
// lower nice) applies to processes started afterwards, since an
// unprivileged process cannot raise its children's priority.
type Nicer struct {
	// Nice is read every period, so changes apply live; 0 leaves
	// processes alone.
	Nice func(ctx context.Context) int
	// Changed, when set, returns a channel closed at the next profile
	// change.
	Changed func() <-chan struct{}
	// Period is how often new processes are looked for; 0 means 1s.
	Period time.Duration
	Log    *slog.Logger
}

// Run keeps the job processes at the profile's nice value until ctx ends.
func (n *Nicer) Run(ctx context.Context) {
	if !NiceSupported() {
		return
	}
	period := n.Period
	if period <= 0 {
		period = time.Second
	}
	self, last := os.Getpid(), 0
	done := map[int]int{} // pid → nice set; pids are not reused within a period or two
	for ctx.Err() == nil {
		changed := n.changed()
		nice := n.Nice(ctx)
		if nice != last {
			n.Log.InfoContext(ctx, "job priority", slog.Int("nice", nice))
			last = nice
		}
		if nice > 0 {
			seen := map[int]int{}
			for _, p := range jobProcs(self) {
				if done[p.pid] != nice {
					renice(p.pid, nice)
				}
				seen[p.pid] = nice
			}
			done = seen
		}
		wait(ctx, period, changed)
	}
}

func (n *Nicer) changed() <-chan struct{} {
	if n.Changed == nil {
		return nil
	}
	return n.Changed()
}
