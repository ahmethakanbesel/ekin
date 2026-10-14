// Package handler implements the system operations of
// gen.StrictServerInterface: capability checks and host profiles.
package handler

import (
	"context"
	"time"

	"kurgu/internal/apperror"
	jobdomain "kurgu/internal/domain/job/v1"
	settingsdomain "kurgu/internal/domain/settings/v1"
	"kurgu/internal/gen"
	settingssvc "kurgu/internal/service/settings/v1"
	"kurgu/internal/transport/http/principal"
)

// Check is one capability check.
type Check struct {
	Name   string
	OK     bool
	Detail string
}

// Handler serves system operations.
type Handler struct {
	settings *settingssvc.Service
	profile  string
	version  string
	checks   func(ctx context.Context) []Check
}

// New returns a Handler. profile is the worker profile of this process.
func New(settings *settingssvc.Service, profile, version string, checks func(ctx context.Context) []Check) *Handler {
	return &Handler{settings: settings, profile: profile, version: version, checks: checks}
}

func (h *Handler) GetSystem(ctx context.Context, _ gen.GetSystemRequestObject) (gen.GetSystemResponseObject, error) {
	out := gen.GetSystem200JSONResponse{Version: h.version, Checks: []gen.SystemCheck{}}
	if h.checks != nil {
		for _, c := range h.checks(ctx) {
			out.Checks = append(out.Checks, gen.SystemCheck{Name: c.Name, Ok: c.OK, Detail: c.Detail})
		}
	}
	return out, nil
}

func (h *Handler) ListProfiles(ctx context.Context, _ gen.ListProfilesRequestObject) (gen.ListProfilesResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	profiles, err := h.settings.Profiles(ctx, h.profile)
	if err != nil {
		return nil, err
	}
	out := gen.ListProfiles200JSONResponse{Current: h.profile, Profiles: []gen.HostProfile{}}
	for _, p := range profiles {
		out.Profiles = append(out.Profiles, profileToWire(p))
	}
	return out, nil
}

func (h *Handler) SaveProfile(ctx context.Context, req gen.SaveProfileRequestObject) (gen.SaveProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	in := settingsdomain.Profile{
		Name: req.Name, Concurrency: map[jobdomain.Queue]int{}, HeavyExclusive: b.HeavyExclusive,
		Cooldown: time.Duration(b.CooldownSeconds) * time.Second, Paused: b.Paused,
		RenderDuty: float64(b.RenderDuty), RenderChunks: b.RenderChunks,
		PreviewScale: float64(b.PreviewScale), FinalScale: float64(b.FinalScale),
	}
	for q, n := range b.Concurrency {
		in.Concurrency[jobdomain.Queue(q)] = n
	}
	saved, err := h.settings.SaveProfile(ctx, in, p.User.ID())
	if err != nil {
		return nil, err
	}
	return gen.SaveProfile200JSONResponse(profileToWire(saved)), nil
}

func requireAdmin(ctx context.Context) error {
	p, err := principal.From(ctx)
	if err != nil {
		return err
	}
	if !p.User.IsAdmin() {
		return apperror.New(apperror.Forbidden, "only the instance admin can see and change host profiles")
	}
	return nil
}

func profileToWire(p settingsdomain.Profile) gen.HostProfile {
	c := map[string]int{}
	for q, n := range p.Concurrency {
		c[string(q)] = n
	}
	return gen.HostProfile{
		Name: p.Name, Concurrency: c, HeavyExclusive: p.HeavyExclusive, CooldownSeconds: int(p.Cooldown / time.Second),
		Paused: p.Paused, RenderDuty: float32(p.RenderDuty), RenderChunks: p.RenderChunks,
		PreviewScale: float32(p.PreviewScale), FinalScale: float32(p.FinalScale),
	}
}
