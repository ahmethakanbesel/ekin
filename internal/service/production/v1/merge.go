package production

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	storydomain "github.com/ahmethakanbesel/ekin/internal/domain/story/v1"
)

// PullState is where a pull request a publisher opened stands.
type PullState string

const (
	PullOpen   PullState = "open"
	PullMerged PullState = "merged"
	PullClosed PullState = "closed" // closed without merging
)

// MergeWatcher is a Publisher whose publications can go out as pull
// requests: it reads where one stands.
type MergeWatcher interface {
	PullRequestState(ctx context.Context, workspaceID uuid.UUID, pr storydomain.PullRequest) (PullState, error)
}

// CheckMerges looks at every deliverable waiting in merging and moves it
// on when its pull request was merged (published; what waited on it is
// released) or closed without merging (failed, so a retry opens a new
// one). It returns how many it moved. A pull request it cannot read is
// logged and left for the next check.
func (s *Service) CheckMerges(ctx context.Context) (int, error) {
	ids, err := s.stories.WithDeliverableStatus(ctx, storydomain.StatusMerging)
	if err != nil {
		return 0, err
	}
	moved := 0
	for _, storyID := range ids {
		st, err := s.stories.GetByID(ctx, storyID)
		if err != nil {
			s.log.WarnContext(ctx, "cannot load a story waiting on a merge", slog.String("story", storyID.String()), slog.Any("error", err))
			continue
		}
		for _, d := range st.Deliverables() {
			if d.Status() != storydomain.StatusMerging {
				continue
			}
			ok, err := s.checkMerge(ctx, st, d)
			if err != nil {
				if ctx.Err() != nil {
					return moved, ctx.Err()
				}
				s.log.WarnContext(ctx, "cannot check a pull request", slog.String("story", st.ID().String()),
					slog.String("deliverable", d.ID().String()), slog.Any("error", err))
				continue
			}
			if ok {
				moved++
			}
		}
	}
	return moved, nil
}

// checkMerge moves d on if its pull request is no longer open and
// reports whether it did.
func (s *Service) checkMerge(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable) (bool, error) {
	pub := d.Publication()
	watcher, ok := s.publishers[d.Kind()].(MergeWatcher)
	if !ok {
		return false, fmt.Errorf("no publisher for %s reads pull requests", d.Kind())
	}
	state, err := watcher.PullRequestState(ctx, st.WorkspaceID(), *pub.PullRequest)
	if err != nil {
		return false, err
	}
	switch state {
	case PullOpen:
		return false, nil
	case PullClosed:
		s.fail(ctx, st, d, fmt.Errorf("pull request #%d was closed without merging", pub.PullRequest.Number))
		return true, nil
	case PullMerged:
	default:
		return false, fmt.Errorf("unknown pull request state %q", state)
	}
	st, err = s.mutate(ctx, st.ID(), func(ctx context.Context, st *storydomain.Story) error {
		if err := st.Merged(d.ID(), s.now()); err != nil {
			return err
		}
		// Derived deliverables can now be drafted around the live piece.
		return s.release(ctx, st, d.ID())
	})
	if err != nil {
		return false, err
	}
	s.notify(ctx, Event{WorkspaceID: st.WorkspaceID(), StoryID: st.ID(), Kind: "published", Title: st.Idea().Title,
		Detail: fmt.Sprintf("%s published: pull request #%d was merged; %s", d.Kind(), pub.PullRequest.Number, pub.URL)})
	return true, nil
}
