package handler

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	storydomain "ekin/internal/domain/story/v1"
	"ekin/internal/gen"
	jobhandler "ekin/internal/transport/http/job/v1"
)

func (h *Handler) ListMoments(ctx context.Context, req gen.ListMomentsRequestObject) (gen.ListMomentsResponseObject, error) {
	m, err := h.svc.Moments(ctx, req.WorkspaceId, req.StoryId, req.DeliverableId)
	if err != nil {
		return nil, err
	}
	l := m.Limits
	out := gen.ListMoments200JSONResponse{
		Moments: []gen.Moment{}, Sentences: []gen.Sentence{}, Shorts: len(m.Story.Shorts(m.Source.ID())),
		Limits: gen.ShortsLimits{
			SourceMinSeconds: l.SourceMin().Seconds(), SourceMaxSeconds: l.SourceMax().Seconds(),
			ClipMinSeconds: l.ClipMin().Seconds(), ClipMaxSeconds: l.ClipMax().Seconds(),
			MaxPerVideo: l.MaxPerVideo(), MinGapSeconds: l.MinGap().Seconds(),
		},
		Blocked: optional(m.Blocked),
	}
	for _, mo := range m.Story.Moments(m.Source.ID()) {
		out.Moments = append(out.Moments, momentToWire(mo))
	}
	for _, s := range m.Sentences {
		out.Sentences = append(out.Sentences, gen.Sentence{Text: s.Text, StartSeconds: s.Start.Seconds(), EndSeconds: s.End.Seconds()})
	}
	return out, nil
}

func (h *Handler) ScanMoments(ctx context.Context, req gen.ScanMomentsRequestObject) (gen.ScanMomentsResponseObject, error) {
	j, err := h.svc.ScanMoments(ctx, req.WorkspaceId, req.StoryId, req.DeliverableId)
	if err != nil {
		return nil, err
	}
	return gen.ScanMoments202JSONResponse(jobhandler.ToWire(j)), nil
}

func (h *Handler) AddMoment(ctx context.Context, req gen.AddMomentRequestObject) (gen.AddMomentResponseObject, error) {
	clip, err := clipOf(req.Body.StartSeconds, req.Body.EndSeconds)
	if err != nil {
		return nil, err
	}
	d := storydomain.MomentDraft{Clip: clip}
	if req.Body.Hook != nil {
		d.Hook = *req.Body.Hook
	}
	_, m, err := h.svc.AddMoment(ctx, req.WorkspaceId, req.StoryId, req.DeliverableId, d)
	if err != nil {
		return nil, err
	}
	return gen.AddMoment201JSONResponse(momentToWire(m)), nil
}

func (h *Handler) AdjustMoment(ctx context.Context, req gen.AdjustMomentRequestObject) (gen.AdjustMomentResponseObject, error) {
	b := req.Body
	var clip *storydomain.Clip
	switch {
	case b.StartSeconds != nil && b.EndSeconds != nil:
		c, err := clipOf(*b.StartSeconds, *b.EndSeconds)
		if err != nil {
			return nil, err
		}
		clip = &c
	case b.StartSeconds != nil || b.EndSeconds != nil:
		return nil, apperror.New(apperror.ValidationFailed, "give both startSeconds and endSeconds to move a moment")
	case b.Hook == nil:
		return nil, apperror.New(apperror.ValidationFailed, "nothing to change")
	}
	_, m, err := h.svc.AdjustMoment(ctx, req.WorkspaceId, req.StoryId, req.MomentId, clip, b.Hook)
	if err != nil {
		return nil, err
	}
	return gen.AdjustMoment200JSONResponse(momentToWire(m)), nil
}

func (h *Handler) DiscardMoment(ctx context.Context, req gen.DiscardMomentRequestObject) (gen.DiscardMomentResponseObject, error) {
	if _, err := h.svc.DiscardMoment(ctx, req.WorkspaceId, req.StoryId, req.MomentId); err != nil {
		return nil, err
	}
	return gen.DiscardMoment204Response{}, nil
}

func (h *Handler) MakeShorts(ctx context.Context, req gen.MakeShortsRequestObject) (gen.MakeShortsResponseObject, error) {
	st, err := h.svc.MakeShorts(ctx, req.WorkspaceId, req.StoryId, req.DeliverableId, req.Body.MomentIds)
	if err != nil {
		return nil, err
	}
	return gen.MakeShorts200JSONResponse(ToWire(st)), nil
}

// clipOf parses a span given in seconds.
func clipOf(start, end float64) (storydomain.Clip, error) {
	if math.IsNaN(start) || math.IsNaN(end) || math.IsInf(start, 0) || math.IsInf(end, 0) || end > 24*3600 {
		return storydomain.Clip{}, apperror.New(apperror.ValidationFailed, "a moment's times must be seconds within the video")
	}
	return storydomain.NewClip(time.Duration(start*float64(time.Second)), time.Duration(end*float64(time.Second)))
}

func clipToWire(c storydomain.Clip) gen.Clip {
	return gen.Clip{StartSeconds: c.Start().Seconds(), EndSeconds: c.End().Seconds()}
}

func momentToWire(m *storydomain.Moment) gen.Moment {
	out := gen.Moment{
		Id: m.ID(), SourceId: m.SourceID(), Clip: clipToWire(m.Clip()), Hook: m.Hook(), Reason: m.Reason(),
		Score: m.Score(), Origin: gen.MomentOrigin(m.Origin()), Status: gen.MomentStatus(m.Status()),
		CreatedAt: m.CreatedAt(), UpdatedAt: m.UpdatedAt(),
	}
	if short := m.ShortID(); short != uuid.Nil {
		out.ShortId = &short
	}
	return out
}
