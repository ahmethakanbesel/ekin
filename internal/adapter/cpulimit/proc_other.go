//go:build !linux && !darwin

package cpulimit

// NiceSupported reports whether this OS can lower job processes' priority.
func NiceSupported() bool { return false }

func jobProcs(int) []procStat { return nil }

func renice(int, int) {}
