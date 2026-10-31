package production_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	pipelinedomain "github.com/ahmethakanbesel/ekin/internal/domain/pipeline/v1"
	storydomain "github.com/ahmethakanbesel/ekin/internal/domain/story/v1"
	production "github.com/ahmethakanbesel/ekin/internal/service/production/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

// prPublisher publishes as a pull request and reports the state it is
// told to.
type prPublisher struct {
	fakePublisher
	state *production.PullState
	err   *error
}

func (p prPublisher) Publish(ctx context.Context, req production.PublishRequest) (storydomain.Publication, error) {
	pub, err := p.fakePublisher.Publish(ctx, req)
	pub.PullRequest = &storydomain.PullRequest{Repo: "o/site", Number: 7, URL: "https://github.com/o/site/pull/7", Branch: "ekin/x"}
	return pub, err
}

func (p prPublisher) PullRequestState(_ context.Context, _ uuid.UUID, pr storydomain.PullRequest) (production.PullState, error) {
	if pr.Number != 7 {
		return "", errors.New("asked about the wrong pull request")
	}
	return *p.state, *p.err
}

// mergingFixture approves the fixture's article and runs its publish job
// through a publisher that opens a pull request.
func mergingFixture(t *testing.T) (fixture, *storydomain.Story, *production.PullState, *error) {
	t.Helper()
	f, st := articleFixture(t)
	state, readErr := production.PullOpen, error(nil)
	f.svc.AddPublisher(prPublisher{fakePublisher{kind: pipelinedomain.KindArticle}, &state, &readErr})
	if _, err := f.svc.Approve(context.Background(), f.ws, st.ID(), st.Lead().ID(), id.New()); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	d := f.story(t).Lead()
	if d.Status() != storydomain.StatusMerging || d.Publication() == nil || d.Publication().PullRequest == nil {
		t.Fatalf("after publishing the article is %s with %+v", d.Status(), d.Publication())
	}
	return f, st, &state, &readErr
}

func TestCheckMerges(t *testing.T) {
	ctx := context.Background()
	t.Run("an open pull request waits", func(t *testing.T) {
		f, _, _, _ := mergingFixture(t)
		if n, err := f.svc.CheckMerges(ctx); err != nil || n != 0 {
			t.Fatalf("check = %d %v", n, err)
		}
		if got := f.story(t).Lead().Status(); got != storydomain.StatusMerging {
			t.Fatalf("article is %s", got)
		}
	})
	t.Run("a merged pull request publishes", func(t *testing.T) {
		f, _, state, _ := mergingFixture(t)
		*state = production.PullMerged
		if n, err := f.svc.CheckMerges(ctx); err != nil || n != 1 {
			t.Fatalf("check = %d %v", n, err)
		}
		d := f.story(t).Lead()
		if d.Status() != storydomain.StatusPublished || d.Publication().URL != "https://example.com/article" {
			t.Fatalf("article is %s at %+v", d.Status(), d.Publication())
		}
		if n, err := f.svc.CheckMerges(ctx); err != nil || n != 0 {
			t.Fatalf("second check = %d %v", n, err)
		}
	})
	t.Run("a closed pull request fails and a retry publishes again", func(t *testing.T) {
		f, st, state, _ := mergingFixture(t)
		*state = production.PullClosed
		if n, err := f.svc.CheckMerges(ctx); err != nil || n != 1 {
			t.Fatalf("check = %d %v", n, err)
		}
		d := f.story(t).Lead()
		if d.Status() != storydomain.StatusFailed || d.FailedStep() != storydomain.StatusMerging || d.Failure() != "pull request #7 was closed without merging" {
			t.Fatalf("article is %s after %s: %q", d.Status(), d.FailedStep(), d.Failure())
		}
		*state = production.PullOpen
		if _, err := f.svc.Retry(ctx, f.ws, st.ID(), d.ID()); err != nil {
			t.Fatal(err)
		}
		f.drain(t)
		if got := f.story(t).Lead().Status(); got != storydomain.StatusMerging {
			t.Fatalf("after the retry the article is %s", got)
		}
	})
	t.Run("a pull request it cannot read is left for the next check", func(t *testing.T) {
		f, _, _, readErr := mergingFixture(t)
		*readErr = errors.New("github: 502")
		if n, err := f.svc.CheckMerges(ctx); err != nil || n != 0 {
			t.Fatalf("check = %d %v", n, err)
		}
		if got := f.story(t).Lead().Status(); got != storydomain.StatusMerging {
			t.Fatalf("article is %s", got)
		}
	})
}
