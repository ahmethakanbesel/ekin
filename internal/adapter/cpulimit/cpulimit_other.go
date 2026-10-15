//go:build !linux

package cpulimit

// Supported reports whether this OS can limit job processes.
func Supported() bool { return false }

type sampler struct{}

func newSampler() *sampler         { return &sampler{} }
func (s *sampler) sample() float64 { return 0 }
func (s *sampler) pause()          {}
func (s *sampler) resume()         {}
