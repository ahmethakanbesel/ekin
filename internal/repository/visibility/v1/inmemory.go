package visibility

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	visibilitydomain "github.com/ahmethakanbesel/ekin/internal/domain/visibility/v1"
)

// InMemory is a Repository for tests.
type InMemory struct {
	mu       sync.RWMutex
	profiles map[uuid.UUID]visibilitydomain.ProfileSnapshot
	prompts  map[uuid.UUID]visibilitydomain.Prompt
	runs     map[uuid.UUID]visibilitydomain.Run
	answers  []answerRow
}

type answerRow struct {
	ws uuid.UUID
	a  visibilitydomain.Answer
}

var _ Repository = (*InMemory)(nil)

// NewInMemory returns an empty InMemory.
func NewInMemory() *InMemory {
	return &InMemory{profiles: map[uuid.UUID]visibilitydomain.ProfileSnapshot{}, prompts: map[uuid.UUID]visibilitydomain.Prompt{},
		runs: map[uuid.UUID]visibilitydomain.Run{}}
}

func (r *InMemory) Profile(_ context.Context, ws uuid.UUID) (*visibilitydomain.Profile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sn, ok := r.profiles[ws]
	if !ok {
		return nil, apperror.New(apperror.NotFound, "AI visibility is not set up")
	}
	return visibilitydomain.RehydrateProfile(sn)
}

func (r *InMemory) PutProfile(_ context.Context, p *visibilitydomain.Profile) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.profiles[p.WorkspaceID()] = p.Snapshot()
	return nil
}

func (r *InMemory) DueProfiles(_ context.Context, now time.Time) ([]*visibilitydomain.Profile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*visibilitydomain.Profile
	for _, sn := range r.profiles {
		p, err := visibilitydomain.RehydrateProfile(sn)
		if err != nil {
			return nil, err
		}
		if p.Due(now) {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b *visibilitydomain.Profile) int { return a.NextRunAt().Compare(b.NextRunAt()) })
	return out, nil
}

func (r *InMemory) Prompts(_ context.Context, ws uuid.UUID) ([]visibilitydomain.Prompt, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []visibilitydomain.Prompt
	for _, p := range r.prompts {
		if p.WorkspaceID == ws {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b visibilitydomain.Prompt) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID.String(), b.ID.String()))
	})
	return out, nil
}

func (r *InMemory) Prompt(_ context.Context, ws, id uuid.UUID) (visibilitydomain.Prompt, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.prompts[id]
	if !ok || p.WorkspaceID != ws {
		return visibilitydomain.Prompt{}, apperror.New(apperror.NotFound, "prompt not found")
	}
	return p, nil
}

func (r *InMemory) PutPrompt(_ context.Context, p visibilitydomain.Prompt) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.prompts[p.ID]; ok {
		p.CreatedAt, p.Source = old.CreatedAt, old.Source
	}
	r.prompts[p.ID] = p
	return nil
}

func (r *InMemory) DeletePrompt(_ context.Context, ws, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.prompts[id]
	if !ok || p.WorkspaceID != ws {
		return apperror.New(apperror.NotFound, "prompt not found")
	}
	delete(r.prompts, id)
	r.answers = slices.DeleteFunc(r.answers, func(x answerRow) bool { return x.a.PromptID == id })
	return nil
}

func (r *InMemory) CreateRun(_ context.Context, run visibilitydomain.Run) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[run.ID] = run
	return nil
}

func (r *InMemory) SaveRun(_ context.Context, run visibilitydomain.Run) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.runs[run.ID]; ok && old.WorkspaceID == run.WorkspaceID {
		run.Trigger, run.CreatedAt = old.Trigger, old.CreatedAt
		r.runs[run.ID] = run
	}
	return nil
}

func (r *InMemory) Run(_ context.Context, ws, id uuid.UUID) (visibilitydomain.Run, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	run, ok := r.runs[id]
	if !ok || run.WorkspaceID != ws {
		return visibilitydomain.Run{}, apperror.New(apperror.NotFound, "run not found")
	}
	return run, nil
}

func (r *InMemory) Runs(_ context.Context, ws uuid.UUID, limit int) ([]visibilitydomain.Run, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []visibilitydomain.Run
	for _, run := range r.runs {
		if run.WorkspaceID == ws {
			out = append(out, run)
		}
	}
	slices.SortFunc(out, func(a, b visibilitydomain.Run) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID.String(), a.ID.String()))
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *InMemory) PutAnswer(_ context.Context, ws uuid.UUID, a visibilitydomain.Answer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a.Citations = slices.Clone(a.Citations)
	r.answers = append(r.answers, answerRow{ws: ws, a: a})
	return nil
}

func (r *InMemory) Answers(_ context.Context, ws, runID uuid.UUID) ([]visibilitydomain.Answer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []visibilitydomain.Answer
	for _, x := range r.answers {
		if x.ws == ws && x.a.RunID == runID {
			out = append(out, x.a)
		}
	}
	slices.SortFunc(out, func(a, b visibilitydomain.Answer) int {
		pa, pb := r.prompts[a.PromptID], r.prompts[b.PromptID]
		return cmp.Or(pa.CreatedAt.Compare(pb.CreatedAt), cmp.Compare(pa.ID.String(), pb.ID.String()), cmp.Compare(a.Model, b.Model))
	})
	return out, nil
}

func (r *InMemory) CallsSince(_ context.Context, ws uuid.UUID, since time.Time) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := 0
	for _, x := range r.answers {
		if x.ws == ws && !x.a.CreatedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

func (r *InMemory) ForgetTexts(_ context.Context, cutoff time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for i := range r.answers {
		if r.answers[i].a.CreatedAt.Before(cutoff) && r.answers[i].a.Text != "" {
			r.answers[i].a.Text = ""
			n++
		}
	}
	return n, nil
}
