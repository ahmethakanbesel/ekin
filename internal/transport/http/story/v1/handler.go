// Package handler implements the story and review operations of
// gen.StrictServerInterface.
package handler

import (
	"context"
	voicehandler "kurgu/internal/transport/http/voice/v1"
	"slices"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	storydomain "kurgu/internal/domain/story/v1"
	"kurgu/internal/gen"
	notifysvc "kurgu/internal/service/notify/v1"
	productionsvc "kurgu/internal/service/production/v1"
	jobhandler "kurgu/internal/transport/http/job/v1"
	pipelinehandler "kurgu/internal/transport/http/pipeline/v1"
	"kurgu/internal/transport/http/principal"
)

// Feed lists a workspace's notifications (the notify service).
type Feed interface {
	Feed(ctx context.Context, workspaceID uuid.UUID, limit int) ([]notifysvc.Notification, error)
}

// Handler serves stories.
type Handler struct {
	svc  *productionsvc.Service
	feed Feed
}

// New returns a Handler.
func New(svc *productionsvc.Service, feed Feed) *Handler { return &Handler{svc: svc, feed: feed} }

func (h *Handler) ListNotifications(ctx context.Context, req gen.ListNotificationsRequestObject) (gen.ListNotificationsResponseObject, error) {
	limit := 50
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	out := gen.ListNotifications200JSONResponse{Notifications: []gen.Notification{}}
	if h.feed == nil {
		return out, nil
	}
	items, err := h.feed.Feed(ctx, req.WorkspaceId, limit)
	if err != nil {
		return nil, err
	}
	for _, n := range items {
		w := gen.Notification{Id: n.ID, Kind: gen.NotificationKind(n.Kind), Title: n.Title, Detail: n.Detail, CreatedAt: n.CreatedAt}
		if n.StoryID != uuid.Nil {
			sid := n.StoryID
			w.StoryId = &sid
		}
		out.Notifications = append(out.Notifications, w)
	}
	return out, nil
}

func (h *Handler) ListStories(ctx context.Context, req gen.ListStoriesRequestObject) (gen.ListStoriesResponseObject, error) {
	f := productionsvc.Filter{}
	if req.Params.Status != nil {
		for _, raw := range *req.Params.Status {
			st, err := storydomain.ParseStatus(string(raw))
			if err != nil {
				return nil, err
			}
			f.Statuses = append(f.Statuses, st)
		}
	}
	if req.Params.PipelineId != nil {
		f.PipelineID = *req.Params.PipelineId
	}
	if req.Params.Limit != nil {
		f.Limit = *req.Params.Limit
	}
	stories, err := h.svc.List(ctx, req.WorkspaceId, f)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Story, 0, len(stories))
	for _, st := range stories {
		out = append(out, ToWire(st))
	}
	return gen.ListStories200JSONResponse{Stories: out}, nil
}

func (h *Handler) CreateStories(ctx context.Context, req gen.CreateStoriesRequestObject) (gen.CreateStoriesResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	lang, err := pipelinedomain.ParseLanguage(string(req.Body.Language))
	if err != nil {
		return nil, err
	}
	outputs, err := pipelinehandler.ParseOutputs(req.Body.Outputs)
	if err != nil {
		return nil, err
	}
	in := productionsvc.AdHocRequest{Idea: req.Body.Idea, Count: req.Body.Count, Language: lang, Outputs: outputs}
	if req.Body.TemplateId != nil {
		in.TemplateID = *req.Body.TemplateId
	}
	in.Voice = voicehandler.OverrideOf(req.Body.Voice)
	if req.Body.Style != nil {
		in.Style = *req.Body.Style
	}
	if req.Body.Refs != nil {
		for _, r := range *req.Body.Refs {
			ref := storydomain.Ref{DocumentID: r.DocumentId}
			if r.FromPage != nil {
				ref.From = *r.FromPage
			}
			if r.ToPage != nil {
				ref.To = *r.ToPage
			}
			if (ref.From == 0) != (ref.To == 0) || ref.To < ref.From {
				return nil, apperror.New(apperror.ValidationFailed, "a page range needs both fromPage and toPage")
			}
			in.Refs = append(in.Refs, ref)
		}
	}
	j, err := h.svc.QueueAdHoc(ctx, req.WorkspaceId, p.User.ID(), in)
	if err != nil {
		return nil, err
	}
	return gen.CreateStories202JSONResponse(jobhandler.ToWire(j)), nil
}

func (h *Handler) GetStory(ctx context.Context, req gen.GetStoryRequestObject) (gen.GetStoryResponseObject, error) {
	st, err := h.svc.Get(ctx, req.WorkspaceId, req.StoryId)
	if err != nil {
		return nil, err
	}
	return gen.GetStory200JSONResponse(ToWire(st)), nil
}

func (h *Handler) DeleteStory(ctx context.Context, req gen.DeleteStoryRequestObject) (gen.DeleteStoryResponseObject, error) {
	if err := h.svc.Delete(ctx, req.WorkspaceId, req.StoryId); err != nil {
		return nil, err
	}
	return gen.DeleteStory204Response{}, nil
}

func (h *Handler) ListRevisions(ctx context.Context, req gen.ListRevisionsRequestObject) (gen.ListRevisionsResponseObject, error) {
	revs, err := h.svc.Revisions(ctx, req.WorkspaceId, req.StoryId, req.DeliverableId)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Revision, 0, len(revs))
	for _, r := range revs {
		names := make([]string, 0, len(r.Artifacts))
		for name := range r.Artifacts {
			names = append(names, name)
		}
		slices.Sort(names)
		rev := gen.Revision{
			Number: r.Number, Feedback: feedbackToWire(r.Feedback), Artifacts: names, Notes: r.Notes, CreatedAt: r.CreatedAt,
		}
		if r.EditedBy != uuid.Nil {
			by := r.EditedBy
			rev.EditedBy = &by
		}
		out = append(out, rev)
	}
	return gen.ListRevisions200JSONResponse{Revisions: out}, nil
}

func (h *Handler) ApproveDeliverable(ctx context.Context, req gen.ApproveDeliverableRequestObject) (gen.ApproveDeliverableResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	st, err := h.svc.Approve(ctx, req.WorkspaceId, req.StoryId, req.DeliverableId, p.User.ID())
	if err != nil {
		return nil, err
	}
	return gen.ApproveDeliverable200JSONResponse(ToWire(st)), nil
}

func (h *Handler) RequestChanges(ctx context.Context, req gen.RequestChangesRequestObject) (gen.RequestChangesResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	var scope storydomain.Scope
	if req.Body.Scope != nil {
		scope = storydomain.Scope(*req.Body.Scope)
	}
	var at *float64
	if req.Body.AtSeconds != nil {
		v := *req.Body.AtSeconds
		at = &v
	}
	fb, err := storydomain.NewFeedback(req.Body.Text, scope, at, p.User.ID(), timeNow())
	if err != nil {
		return nil, err
	}
	st, err := h.svc.RequestChanges(ctx, req.WorkspaceId, req.StoryId, req.DeliverableId, fb)
	if err != nil {
		return nil, err
	}
	return gen.RequestChanges200JSONResponse(ToWire(st)), nil
}

func (h *Handler) RejectDeliverable(ctx context.Context, req gen.RejectDeliverableRequestObject) (gen.RejectDeliverableResponseObject, error) {
	st, err := h.svc.Reject(ctx, req.WorkspaceId, req.StoryId, req.DeliverableId)
	if err != nil {
		return nil, err
	}
	return gen.RejectDeliverable200JSONResponse(ToWire(st)), nil
}

func (h *Handler) RetryDeliverable(ctx context.Context, req gen.RetryDeliverableRequestObject) (gen.RetryDeliverableResponseObject, error) {
	st, err := h.svc.Retry(ctx, req.WorkspaceId, req.StoryId, req.DeliverableId)
	if err != nil {
		return nil, err
	}
	return gen.RetryDeliverable200JSONResponse(ToWire(st)), nil
}

func (h *Handler) PublishDeliverable(ctx context.Context, req gen.PublishDeliverableRequestObject) (gen.PublishDeliverableResponseObject, error) {
	st, err := h.svc.Publish(ctx, req.WorkspaceId, req.StoryId, req.DeliverableId)
	if err != nil {
		return nil, err
	}
	return gen.PublishDeliverable200JSONResponse(ToWire(st)), nil
}

// ToWire converts a story.
func ToWire(st *storydomain.Story) gen.Story {
	idea := st.Idea()
	out := gen.Story{
		Id: st.ID(), Origin: gen.StoryOrigin(st.Origin()), Language: gen.Language(st.Language()),
		Title: idea.Title, Angle: idea.Angle, Keywords: append([]string{}, idea.Keywords...),
		Status: gen.DeliverableStatus(st.Status()), CreatedAt: st.CreatedAt(), UpdatedAt: st.UpdatedAt(),
	}
	if st.PipelineID() != uuid.Nil {
		pid := st.PipelineID()
		out.PipelineId = &pid
	}
	if ins := st.Brief().Instructions; ins != "" {
		out.Instructions = &ins
	}
	if t := st.Brief().Template; t != nil {
		name := t.Name
		out.TemplateName = &name
	}
	if kw := st.Brief().Keyword; kw != "" {
		out.Keyword = &kw
	}
	if refs := st.Brief().Refs; len(refs) > 0 {
		wire := make([]gen.KnowledgeRef, 0, len(refs))
		for _, r := range refs {
			ref := gen.KnowledgeRef{DocumentId: r.DocumentID}
			if r.From > 0 {
				from, to := r.From, r.To
				ref.FromPage, ref.ToPage = &from, &to
			}
			wire = append(wire, ref)
		}
		out.Refs = &wire
	}
	for _, d := range st.Deliverables() {
		wd := gen.Deliverable{
			Id: d.ID(), Kind: gen.DeliverableKind(d.Kind()), Role: gen.DeliverableRole(d.Role()),
			Language: gen.Language(d.Language()), Status: gen.DeliverableStatus(d.Status()), Revision: d.Revision(),
			PendingFeedback: feedbackToWire(d.PendingFeedback()), CreatedAt: d.CreatedAt(), UpdatedAt: d.UpdatedAt(),
		}
		if d.SourceID() != uuid.Nil {
			sid := d.SourceID()
			wd.SourceId = &sid
		}
		if v := d.Video(); v != nil {
			wd.Video = pipelinehandler.VideoToWire(*v)
		}
		if a := d.Article(); a != nil {
			wd.Article = pipelinehandler.ArticleToWire(*a)
		}
		if f := d.Failure(); f != "" {
			wd.Failure = &f
			step := string(d.FailedStep())
			wd.FailedStep = &step
		}
		if at := d.ApprovedAt(); !at.IsZero() {
			wd.ApprovedAt = &at
		}
		if pub := d.Publication(); pub != nil {
			wd.Publication = &gen.Publication{Url: pub.URL, ExternalId: pub.ExternalID, At: pub.At}
		}
		out.Deliverables = append(out.Deliverables, wd)
	}
	return out
}

func feedbackToWire(fb *storydomain.Feedback) *gen.Feedback {
	if fb == nil {
		return nil
	}
	out := &gen.Feedback{Text: fb.Text, Scope: gen.FeedbackScope(fb.Scope), At: fb.At}
	if fb.AtSeconds != nil {
		v := *fb.AtSeconds
		out.AtSeconds = &v
	}
	return out
}

var timeNow = time.Now

func (h *Handler) GetDeliverableContent(ctx context.Context, req gen.GetDeliverableContentRequestObject) (gen.GetDeliverableContentResponseObject, error) {
	c, err := h.svc.Content(ctx, req.WorkspaceId, req.StoryId, req.DeliverableId)
	if err != nil {
		return nil, err
	}
	out := gen.GetDeliverableContent200JSONResponse{Revision: c.Revision, Article: c.Article, Narration: c.Narration}
	if m := c.Metadata; m != nil {
		tags := m.Tags
		if tags == nil {
			tags = []string{}
		}
		out.Metadata = &gen.VideoMetadataContent{Title: m.Title, Description: m.Description, Tags: tags}
	}
	return out, nil
}

func (h *Handler) EditDeliverableContent(ctx context.Context, req gen.EditDeliverableContentRequestObject) (gen.EditDeliverableContentResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	e := productionsvc.Edit{Revision: req.Body.Revision, Article: req.Body.Article, Narration: req.Body.Narration}
	if m := req.Body.Metadata; m != nil {
		e.Metadata = &productionsvc.VideoMetadata{Title: m.Title, Description: m.Description, Tags: m.Tags}
	}
	st, err := h.svc.EditContent(ctx, req.WorkspaceId, req.StoryId, req.DeliverableId, p.User.ID(), e)
	if err != nil {
		return nil, err
	}
	return gen.EditDeliverableContent200JSONResponse(ToWire(st)), nil
}
