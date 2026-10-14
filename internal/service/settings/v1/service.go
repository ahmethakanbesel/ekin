// Package settings serves host profiles: the runtime throttle and render
// settings workers read before every claim and render.
package settings

import (
	"context"
	"encoding/json"
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
	"kurgu/pkg/secretbox"
)

// cacheFor is how long a worker trusts a profile it read; edits apply
// within this delay.
const cacheFor = 10 * time.Second

// Service reads and writes host profiles and credentials.
type Service struct {
	repo     settingsrepo.Repository
	defaults settingsdomain.Profile // from the environment
	log      *slog.Logger
	now      func() time.Time

	mu     sync.Mutex
	cached map[string]cachedProfile

	box      *secretbox.Box
	envCreds settingsdomain.Credentials
	defCreds settingsdomain.Credentials
	credsAt  time.Time
	creds    settingsdomain.Credentials
}

// WithCredentials enables credentials: env holds the KURGU_* values,
// box seals what the settings page stores.
func (s *Service) WithCredentials(env settingsdomain.Credentials, box *secretbox.Box) *Service {
	s.envCreds, s.box = env, box
	return s
}

// WithCredentialDefaults sets the built-in values used when neither the
// settings page nor the environment sets a credential.
func (s *Service) WithCredentialDefaults(d settingsdomain.Credentials) *Service {
	s.defCreds = d
	return s
}

const credentialsKey = "credentials"

type sealedDoc struct {
	Sealed string `json:"sealed"`
}

// stored returns the credentials saved from the settings page.
func (s *Service) stored(ctx context.Context) (settingsdomain.Credentials, error) {
	var doc sealedDoc
	err := s.repo.Get(ctx, credentialsKey, &doc)
	if apperror.Is(err, apperror.NotFound) || s.box == nil {
		return settingsdomain.Credentials{}, nil
	}
	if err != nil {
		return settingsdomain.Credentials{}, err
	}
	plain, err := s.box.Open(doc.Sealed)
	if err != nil {
		return settingsdomain.Credentials{}, err
	}
	var c settingsdomain.Credentials
	return c, json.Unmarshal([]byte(plain), &c)
}

// Credentials returns the effective credentials (settings page over
// environment), re-read at most every cacheFor.
func (s *Service) Credentials(ctx context.Context) settingsdomain.Credentials {
	s.mu.Lock()
	if !s.credsAt.IsZero() && s.now().Sub(s.credsAt) < cacheFor {
		c := s.creds
		s.mu.Unlock()
		return c
	}
	s.mu.Unlock()
	stored, err := s.stored(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "read credentials", slog.Any("error", err))
	}
	c := s.defCreds.Merge(s.envCreds).Merge(stored)
	s.mu.Lock()
	s.creds, s.credsAt = c, s.now()
	s.mu.Unlock()
	return c
}

// CredentialStatus is one credential as the settings page sees it.
type CredentialStatus struct {
	Field  settingsdomain.CredentialField
	Source string // settings | env | default | unset
	Value  string // plain for non-secrets, a hint for secrets
}

// CredentialStatuses describes every credential without revealing secrets.
func (s *Service) CredentialStatuses(ctx context.Context) ([]CredentialStatus, error) {
	stored, err := s.stored(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CredentialStatus, 0, len(settingsdomain.Fields))
	for _, f := range settingsdomain.Fields {
		st := CredentialStatus{Field: f, Source: "unset"}
		v := f.Value(stored)
		if v != "" {
			st.Source = "settings"
		} else if v = f.Value(s.envCreds); v != "" {
			st.Source = "env"
		} else if v = f.Value(s.defCreds); v != "" {
			st.Source = "default"
		}
		if v != "" {
			st.Value = v
			if f.Secret {
				st.Value = settingsdomain.Hint(v)
			}
		}
		out = append(out, st)
	}
	return out, nil
}

// UpdateCredentials applies patch (key → value; "" removes the saved
// value so the environment applies again) and stores the result sealed.
func (s *Service) UpdateCredentials(ctx context.Context, patch map[string]string, by uuid.UUID) error {
	if s.box == nil {
		return apperror.New(apperror.InternalError, "credentials are not enabled")
	}
	c, err := s.stored(ctx)
	if err != nil {
		return err
	}
	if err := c.Apply(patch); err != nil {
		return err
	}
	raw, _ := json.Marshal(c)
	if err := s.repo.Put(ctx, credentialsKey, sealedDoc{Sealed: s.box.Seal(string(raw))}, by, s.now()); err != nil {
		return err
	}
	s.mu.Lock()
	s.credsAt = time.Time{}
	s.mu.Unlock()
	return nil
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
