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
	CreatedAt, UpdatedAt     time.Time
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
			Published: d.published, CreatedAt: d.createdAt, UpdatedAt: d.updatedAt,
		}
		if d.video != nil {
			v := pipelinedomain.SnapshotVideo(*d.video)
			ds.Video = &v
		}
		if d.article != nil {
			a := pipelinedomain.SnapshotArticle(*d.article)
			ds.Article = &a
		}
		out.Deliverables = append(out.Deliverables, ds)
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
		d := &Deliverable{
			id: ds.ID, kind: kind, role: Role(ds.Role), sourceID: ds.SourceID, language: dl,
			status: status, failedStep: Status(ds.FailedStep), revision: ds.Revision, pending: ds.Pending,
			failure: ds.Failure, session: ds.Session, approvedBy: ds.ApprovedBy,
			approvedAt: timeutil.Normalize(ds.ApprovedAt), published: ds.Published,
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
		st.deliverables = append(st.deliverables, d)
	}
	return st, nil
}

// Bump records a successful save, for optimistic concurrency.
func (s *Story) Bump() { s.version++ }
