// Package defaults serves a workspace's production defaults: the layer
// between the server's values and a pipeline's or an ad-hoc request's
// settings (pipelinedomain.Production.Over). It also reports the server's
// values, which apply wherever no layer sets one.
package defaults

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	settingsdomain "kurgu/internal/domain/settings/v1"
	settingsrepo "kurgu/internal/repository/settings/v1"
	settingssvc "kurgu/internal/service/settings/v1"
)

// Settings is the part of the settings service the server's values come
// from: the credentials (the drafting model) and host profiles (render
// quality and frame rates).
type Settings interface {
	CredentialStatuses(ctx context.Context) ([]settingssvc.CredentialStatus, error)
	Profile(ctx context.Context, name string) (settingsdomain.Profile, error)
}

// Config names the server's own settings.
type Config struct {
	// Profile is the host profile this server renders with.
	Profile string
	// Effort is the drafting effort from KURGU_CLAUDE_EFFORT; "" leaves
	// Claude Code's own.
	Effort string
}

// ModelSource is where the server's drafting model comes from.
type ModelSource string

const (
	ModelFromSettings ModelSource = "settings" // System › Credentials
	ModelFromEnv      ModelSource = "env"      // KURGU_CLAUDE_MODEL
	ModelUnset        ModelSource = "unset"    // Claude Code's own default
)

// Server is what stories get where neither the workspace nor a pipeline
// or request sets a value.
type Server struct {
	// DraftModel is the model drafts run with; "" for Claude Code's own.
	DraftModel       string
	DraftModelSource ModelSource
	// DraftEffort is the effort drafts run with; "" for Claude Code's own.
	DraftEffort pipelinedomain.Effort
	// PreviewHeight and FinalHeight are the short side of the review and
	// final renders of a 1080p composition, in pixels.
	PreviewHeight, FinalHeight int
	PreviewFPS, FinalFPS       int
	// Profile names the host profile the render values come from.
	Profile string
}

// Service reads and writes workspace production defaults.
type Service struct {
	repo     settingsrepo.Repository
	settings Settings
	cfg      Config
	now      func() time.Time
}

// NewService returns a Service. settings may be nil, leaving the server's
// values at the built-in ones.
func NewService(repo settingsrepo.Repository, settings Settings, cfg Config) *Service {
	return &Service{repo: repo, settings: settings, cfg: cfg, now: time.Now}
}

func key(ws uuid.UUID) string { return "production:" + ws.String() }

// Get returns the workspace's defaults; zero when it set none.
func (s *Service) Get(ctx context.Context, workspaceID uuid.UUID) (pipelinedomain.Production, error) {
	var p pipelinedomain.Production
	err := s.repo.Get(ctx, key(workspaceID), &p)
	if apperror.Is(err, apperror.NotFound) {
		return pipelinedomain.Production{}, nil
	}
	return p, err
}

// Put validates and stores the workspace's defaults. Reviewing scripts
// first is allowed here: stories without a video review as usual.
func (s *Service) Put(ctx context.Context, workspaceID uuid.UUID, p pipelinedomain.Production, by uuid.UUID) (pipelinedomain.Production, error) {
	if err := p.Check(); err != nil {
		return pipelinedomain.Production{}, err
	}
	return p, s.repo.Put(ctx, key(workspaceID), p, by, s.now())
}

// Server returns the server's values.
func (s *Service) Server(ctx context.Context) (Server, error) {
	out := Server{DraftModelSource: ModelUnset, DraftEffort: pipelinedomain.Effort(s.cfg.Effort), Profile: s.cfg.Profile}
	profile := settingsdomain.Profile{Name: s.cfg.Profile}
	if s.settings != nil {
		statuses, err := s.settings.CredentialStatuses(ctx)
		if err != nil {
			return Server{}, err
		}
		for _, st := range statuses {
			if st.Field.Key != "claudeModel" || st.Value == "" {
				continue
			}
			switch src := ModelSource(st.Source); src {
			case ModelFromSettings, ModelFromEnv:
				out.DraftModel, out.DraftModelSource = st.Value, src
			}
		}
		if profile, err = s.settings.Profile(ctx, s.cfg.Profile); err != nil {
			return Server{}, err
		}
	}
	out.PreviewHeight = height(profile.PreviewScale, 0.5)
	out.FinalHeight = height(profile.FinalScale, 1)
	out.PreviewFPS, out.FinalFPS = profile.PreviewRate(), profile.FinalRate()
	return out, nil
}

// height is the short side of a 1080p composition rendered at scale
// (fallback when the profile has none).
func height(scale, fallback float64) int {
	if scale <= 0 {
		scale = fallback
	}
	return int(math.Round(scale * 1080))
}
