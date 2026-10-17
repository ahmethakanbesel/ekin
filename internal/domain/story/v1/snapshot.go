package story

import (
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	"kurgu/pkg/timeutil"
)

// Snapshot is the persisted form of a Story.
type Snapshot struct {
	ID, WorkspaceID, PipelineID, CreatedBy uuid.UUID
	TraceID                                uuid.UUID
	Origin, Language                       string
	Idea                                   Idea
	Brief                                  Brief
	Deliverables                           []DeliverableSnapshot
	Moments                                []MomentSnapshot
	CreatedAt, UpdatedAt                   time.Time
	Version                                int64
}

// DeliverableSnapshot is the persisted form of a Deliverable.
type DeliverableSnapshot struct {
	ID, SourceID, ApprovedBy uuid.UUID
	Kind, Role, Language     string
	Video                    *pipelinedomain.VideoSnapshot
	Article                  *pipelinedomain.ArticleSnapshot
	Status, FailedStep       string
	Revision                 int
	Pending                  *Feedback
	Failure, Session         string
	ApprovedAt               time.Time
	Published                *Publication
	PublishJob               uuid.UUID
	Measures                 Measures
	ScriptApprovedAt         time.Time
	Clip                     *ClipSnapshot
	Thumbnail                int
	CreatedAt, UpdatedAt     time.Time
}

// ClipSnapshot is the persisted form of a Clip.
type ClipSnapshot struct {
	Start, End time.Duration
}

// MomentSnapshot is the persisted form of a Moment.
type MomentSnapshot struct {
	ID, SourceID, ShortID uuid.UUID
	Clip                  ClipSnapshot
	Hook, Reason          string
	Score                 int
	Origin, Status        string
	CreatedAt, UpdatedAt  time.Time
}

// Snapshot returns s's persisted form.
func (s *Story) Snapshot() Snapshot {
	out := Snapshot{
		ID: s.id, WorkspaceID: s.workspaceID, PipelineID: s.pipelineID, CreatedBy: s.createdBy, TraceID: s.traceID,
		Origin: string(s.origin), Language: string(s.language), Idea: s.idea, Brief: s.brief,
		CreatedAt: s.createdAt, UpdatedAt: s.updatedAt, Version: s.version,
	}
	for _, d := range s.deliverables {
		ds := DeliverableSnapshot{
			ID: d.id, SourceID: d.sourceID, ApprovedBy: d.approvedBy,
			Kind: string(d.kind), Role: string(d.role), Language: string(d.language),
			Status: string(d.status), FailedStep: string(d.failedStep), Revision: d.revision,
			Pending: d.pending, Failure: d.failure, Session: d.session, ApprovedAt: d.approvedAt,
			Published: d.published, PublishJob: d.publishJob, Measures: d.measures, ScriptApprovedAt: d.scriptApprovedAt,
			Thumbnail: d.thumbnail, CreatedAt: d.createdAt, UpdatedAt: d.updatedAt,
		}
		if d.video != nil {
			v := pipelinedomain.SnapshotVideo(*d.video)
			ds.Video = &v
		}
		if d.article != nil {
			a := pipelinedomain.SnapshotArticle(*d.article)
			ds.Article = &a
		}
		if d.clip != nil {
			ds.Clip = &ClipSnapshot{Start: d.clip.start, End: d.clip.end}
		}
		out.Deliverables = append(out.Deliverables, ds)
	}
	for _, m := range s.moments {
		out.Moments = append(out.Moments, MomentSnapshot{
			ID: m.id, SourceID: m.sourceID, ShortID: m.shortID, Clip: ClipSnapshot{Start: m.clip.start, End: m.clip.end},
			Hook: m.hook, Reason: m.reason, Score: m.score, Origin: string(m.origin), Status: string(m.status),
			CreatedAt: m.createdAt, UpdatedAt: m.updatedAt,
		})
	}
	return out
}

// Rehydrate rebuilds a Story from storage.
func Rehydrate(s Snapshot) (*Story, error) {
	origin, err := ParseOrigin(s.Origin)
	if err != nil {
		return nil, err
	}
	lang, err := pipelinedomain.ParseLanguage(s.Language)
	if err != nil {
		return nil, err
	}
	st := &Story{
		id: s.ID, workspaceID: s.WorkspaceID, pipelineID: s.PipelineID, createdBy: s.CreatedBy, traceID: s.TraceID,
		origin: origin, language: lang, idea: s.Idea, brief: s.Brief,
		createdAt: timeutil.Normalize(s.CreatedAt), updatedAt: timeutil.Normalize(s.UpdatedAt), version: s.Version,
	}
	for _, ds := range s.Deliverables {
		kind, err := pipelinedomain.ParseKind(ds.Kind)
		if err != nil {
			return nil, err
		}
		dl, err := pipelinedomain.ParseLanguage(ds.Language)
		if err != nil {
			return nil, err
		}
		status, err := ParseStatus(ds.Status)
		if err != nil {
			return nil, err
		}
		switch Role(ds.Role) {
		case RoleLead, RoleDerived, RoleTranslation:
		default:
			return nil, apperror.New(apperror.InternalError, "unknown deliverable role")
		}
		if ds.Measures.Duration < 0 || ds.Measures.Words < 0 {
			return nil, apperror.New(apperror.InternalError, "negative deliverable measures")
		}
		if ds.Thumbnail < 0 || ds.Thumbnail > MaxThumbnails {
			return nil, apperror.New(apperror.InternalError, "invalid picked thumbnail")
		}
		d := &Deliverable{
			id: ds.ID, kind: kind, role: Role(ds.Role), sourceID: ds.SourceID, language: dl,
			status: status, failedStep: Status(ds.FailedStep), revision: ds.Revision, pending: ds.Pending,
			failure: ds.Failure, session: ds.Session, approvedBy: ds.ApprovedBy,
			approvedAt: timeutil.Normalize(ds.ApprovedAt), published: ds.Published, publishJob: ds.PublishJob, measures: ds.Measures,
			scriptApprovedAt: timeutil.Normalize(ds.ScriptApprovedAt), thumbnail: ds.Thumbnail,
			createdAt: timeutil.Normalize(ds.CreatedAt), updatedAt: timeutil.Normalize(ds.UpdatedAt),
		}
		if ds.Video != nil {
			v, err := pipelinedomain.VideoFromSnapshot(*ds.Video)
			if err != nil {
				return nil, err
			}
			d.video = &v
		}
		if ds.Article != nil {
			a, err := pipelinedomain.ArticleFromSnapshot(*ds.Article)
			if err != nil {
				return nil, err
			}
			d.article = &a
		}
		if ds.Clip != nil {
			c, err := NewClip(ds.Clip.Start, ds.Clip.End)
			if err != nil {
				return nil, apperror.New(apperror.InternalError, "invalid deliverable clip")
			}
			if d.video == nil || d.video.Format() != pipelinedomain.FormatShort || d.sourceID == uuid.Nil {
				return nil, apperror.New(apperror.InternalError, "a clip on a deliverable that is not a derived short")
			}
			d.clip = &c
		}
		st.deliverables = append(st.deliverables, d)
	}
	for _, ms := range s.Moments {
		m, err := rehydrateMoment(ms)
		if err != nil {
			return nil, err
		}
		if _, err := st.Deliverable(m.sourceID); err != nil {
			return nil, apperror.New(apperror.InternalError, "a moment of a deliverable the story does not have")
		}
		st.moments = append(st.moments, m)
	}
	return st, nil
}

func rehydrateMoment(ms MomentSnapshot) (*Moment, error) {
	c, err := NewClip(ms.Clip.Start, ms.Clip.End)
	if err != nil {
		return nil, apperror.New(apperror.InternalError, "invalid moment clip")
	}
	origin, status := MomentOrigin(ms.Origin), MomentStatus(ms.Status)
	switch origin {
	case MomentByAgent, MomentByPerson:
	default:
		return nil, apperror.New(apperror.InternalError, "unknown moment origin")
	}
	switch status {
	case MomentSuggested, MomentDiscarded:
	case MomentMade:
		if ms.ShortID == uuid.Nil {
			return nil, apperror.New(apperror.InternalError, "a made moment without its short")
		}
	default:
		return nil, apperror.New(apperror.InternalError, "unknown moment status")
	}
	if ms.ID == uuid.Nil || ms.SourceID == uuid.Nil || ms.Score < 0 || ms.Score > 100 {
		return nil, apperror.New(apperror.InternalError, "invalid moment")
	}
	return &Moment{
		id: ms.ID, sourceID: ms.SourceID, clip: c, hook: ms.Hook, reason: ms.Reason, score: ms.Score,
		origin: origin, status: status, shortID: ms.ShortID,
		createdAt: timeutil.Normalize(ms.CreatedAt), updatedAt: timeutil.Normalize(ms.UpdatedAt),
	}, nil
}

// Bump records a successful save, for optimistic concurrency.
func (s *Story) Bump() { s.version++ }
