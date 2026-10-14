// Package settings serves host profiles: the runtime throttle and render
// settings workers read before every claim and render.
package settings

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	settingsdomain "kurgu/internal/domain/settings/v1"
	settingsrepo "kurgu/internal/repository/settings/v1"
	jobsvc "kurgu/internal/service/job/v1"
)

// cacheFor is how long a worker trusts a profile it read; edits apply
// within this delay.
const cacheFor = 10 * time.Second

// Service reads and writes host profiles.
type Service struct {
	repo     settingsrepo.Repository
	defaults settingsdomain.Profile // from the environment
	log      *slog.Logger
	now      func() time.Time

	mu     sync.Mutex
	cached map[string]cachedProfile
}

type cachedProfile struct {
	p  settingsdomain.Profile
	at time.Time
}

// NewService returns a Service. defaults (built from KURGU_* variables)
// apply to any profile not saved yet.
func NewService(repo settingsrepo.Repository, defaults settingsdomain.Profile, log *slog.Logger) *Service {
	return &Service{repo: repo, defaults: defaults, log: log, now: time.Now, cached: map[string]cachedProfile{}}
}

// Profile returns the named profile, or the defaults under that name.
func (s *Service) Profile(ctx context.Context, name string) (settingsdomain.Profile, error) {
	var p settingsdomain.Profile
	err := s.repo.Get(ctx, "profile:"+name, &p)
	if apperror.Is(err, apperror.NotFound) {
		p = s.defaults
		p.Name = name
		return p, nil
	}
	return p, err
}

// Profiles lists saved profiles, plus the defaults when unsaved.
func (s *Service) Profiles(ctx context.Context, current string) ([]settingsdomain.Profile, error) {
	keys, err := s.repo.Keys(ctx, "profile:")
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, k := range keys {
		names = append(names, strings.TrimPrefix(k, "profile:"))
	}
	if current != "" && !slices.Contains(names, current) {
		names = append(names, current)
	}
	slices.Sort(names)
	out := make([]settingsdomain.Profile, 0, len(names))
	for _, n := range names {
		p, err := s.Profile(ctx, n)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// SaveProfile validates and stores p.
func (s *Service) SaveProfile(ctx context.Context, p settingsdomain.Profile, by uuid.UUID) (settingsdomain.Profile, error) {
	if err := p.Validate(); err != nil {
		return p, err
	}
	if err := s.repo.Put(ctx, p.Key(), p, by, s.now()); err != nil {
		return p, err
	}
	s.mu.Lock()
	delete(s.cached, p.Name)
	s.mu.Unlock()
	return p, nil
}

// Cached returns the profile, re-reading it at most every cacheFor. A
// read error keeps the last good value (or the defaults).
func (s *Service) Cached(ctx context.Context, name string) settingsdomain.Profile {
	s.mu.Lock()
	c, ok := s.cached[name]
	s.mu.Unlock()
	if ok && s.now().Sub(c.at) < cacheFor {
		return c.p
	}
	p, err := s.Profile(ctx, name)
	if err != nil {
		s.log.WarnContext(ctx, "read host profile", slog.String("profile", name), slog.Any("error", err))
		if ok {
			return c.p
		}
		p = s.defaults
	}
	s.mu.Lock()
	s.cached[name] = cachedProfile{p: p, at: s.now()}
	s.mu.Unlock()
	return p
}

// ThrottleSource adapts a profile to the job runner.
func (s *Service) ThrottleSource(name string) func(ctx context.Context) jobsvc.Throttle {
	return func(ctx context.Context) jobsvc.Throttle {
		p := s.Cached(ctx, name)
		return jobsvc.Throttle{Concurrency: p.Concurrency, HeavyExclusive: p.HeavyExclusive, Cooldown: p.Cooldown, Paused: p.Paused}
	}
}
