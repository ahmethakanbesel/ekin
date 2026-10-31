package site

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	sitedomain "github.com/ahmethakanbesel/ekin/internal/domain/site/v1"
	siterepo "github.com/ahmethakanbesel/ekin/internal/repository/site/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

// KindCompetitorCheck compares one competitor's sitemaps with the pages
// known so far.
const KindCompetitorCheck = "competitor.check"

// CompetitorPayload addresses a competitor.
type CompetitorPayload struct {
	WorkspaceID  uuid.UUID `json:"workspaceId"`
	CompetitorID uuid.UUID `json:"competitorId"`
}

// maxTitles bounds how many new pages a check reads the titles of.
const maxTitles = 20

// ChangeFilter narrows a listing of competitor changes.
type ChangeFilter = siterepo.ChangeFilter

// Competitors returns the workspace's competitors.
func (s *Service) Competitors(ctx context.Context, ws uuid.UUID) ([]*sitedomain.Competitor, error) {
	return s.repo.Competitors(ctx, ws)
}

// AddCompetitor follows a competitor and queues its first check, which
// records its pages as they are.
func (s *Service) AddCompetitor(ctx context.Context, ws uuid.UUID, name, address string) (*sitedomain.Competitor, error) {
	var c *sitedomain.Competitor
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		list, err := s.repo.Competitors(ctx, ws)
		if err != nil {
			return err
		}
		if len(list) >= sitedomain.MaxCompetitors {
			return apperror.New(apperror.ValidationFailed, fmt.Sprintf("a workspace follows at most %d competitors", sitedomain.MaxCompetitors))
		}
		if c, err = sitedomain.NewCompetitor(id.New(), ws, name, address, s.now()); err != nil {
			return err
		}
		if err := s.repo.PutCompetitor(ctx, c); err != nil {
			return err
		}
		return s.enqueueCheck(ctx, c, "")
	})
	return c, err
}

// RemoveCompetitor stops following a competitor and forgets its pages.
func (s *Service) RemoveCompetitor(ctx context.Context, ws, competitorID uuid.UUID) error {
	return s.repo.DeleteCompetitor(ctx, ws, competitorID)
}

// CheckCompetitor queues a check now.
func (s *Service) CheckCompetitor(ctx context.Context, ws, competitorID uuid.UUID) (*sitedomain.Competitor, error) {
	c, err := s.repo.Competitor(ctx, ws, competitorID)
	if err != nil {
		return nil, err
	}
	return c, s.enqueueCheck(ctx, c, "")
}

// CompetitorChanges returns a page of the pages competitors added or
// dropped, newest first.
func (s *Service) CompetitorChanges(ctx context.Context, ws uuid.UUID, f ChangeFilter) ([]sitedomain.CompetitorChange, string, error) {
	return s.repo.CompetitorChanges(ctx, ws, f)
}

// enqueueCheck queues a check of c; slot, when set, keeps one per
// scheduled turn.
func (s *Service) enqueueCheck(ctx context.Context, c *sitedomain.Competitor, slot string) error {
	payload, _ := json.Marshal(CompetitorPayload{WorkspaceID: c.WorkspaceID(), CompetitorID: c.ID()})
	spec := jobdomain.Spec{WorkspaceID: c.WorkspaceID(), Queue: jobdomain.QueueIO, Kind: KindCompetitorCheck, Payload: payload,
		MaxAttempts: 2, ConcurrencyKey: KindCompetitorCheck + ":" + c.ID().String()}
	if slot != "" {
		spec.UniqueKey = KindCompetitorCheck + ":" + c.ID().String() + ":" + slot
	}
	_, _, err := s.jobs.Enqueue(ctx, spec)
	return err
}

// triggerCompetitors queues the checks that are due, once per turn.
func (s *Service) triggerCompetitors(ctx context.Context) (int, error) {
	due, err := s.repo.DueCompetitors(ctx, s.now())
	if err != nil {
		return 0, err
	}
	n := 0
	for _, c := range due {
		if err := s.enqueueCheck(ctx, c, strconv.FormatInt(c.NextCheckAt().UnixMilli(), 10)); err != nil {
			s.log.WarnContext(ctx, "cannot queue a competitor check", slog.String("competitor", c.ID().String()), slog.Any("error", err))
			continue
		}
		n++
	}
	return n, nil
}

// RunCompetitorCheck runs a competitor.check job: it lists the
// competitor's pages from its sitemaps, reads the titles of the new ones
// and records what was added or dropped since the last check. The first
// check only records the pages.
func (s *Service) RunCompetitorCheck(ctx context.Context, p CompetitorPayload) error {
	c, err := s.repo.Competitor(ctx, p.WorkspaceID, p.CompetitorID)
	if apperror.Is(err, apperror.NotFound) {
		return jobdomain.Permanent(err) // removed meanwhile
	}
	if err != nil {
		return err
	}
	listed, err := s.crawler.Sitemap(ctx, c.URL(), s.cfg.Delay)
	if err == nil && len(listed.Pages) == 0 {
		err = fmt.Errorf("no sitemap lists any page on %s", c.URL())
	}
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		return s.tx.WithinTx(context.WithoutCancel(ctx), func(ctx context.Context) error {
			c, err2 := s.repo.Competitor(ctx, p.WorkspaceID, p.CompetitorID)
			if err2 != nil {
				return err2
			}
			c.CheckFailed(err.Error(), s.now())
			return s.repo.PutCompetitor(ctx, c)
		})
	}
	known, err := s.repo.CompetitorPages(ctx, c.ID())
	if err != nil {
		return err
	}
	baseline := c.Baseline()
	pages, changes := sitedomain.DiffPages(known, listed.Pages, baseline, c.WorkspaceID(), c.ID(), id.New, s.now())
	var fresh []string
	for _, ch := range changes {
		if ch.Kind == sitedomain.PageAdded && len(fresh) < maxTitles {
			fresh = append(fresh, ch.URL)
		}
	}
	if len(fresh) > 0 {
		titles, err := s.crawler.Titles(ctx, c.URL(), fresh, s.cfg.Delay)
		if err != nil && ctx.Err() != nil {
			return err
		}
		for i := range changes {
			changes[i].Title = titles[changes[i].URL]
		}
		for i := range pages {
			if t, ok := titles[pages[i].URL]; ok {
				pages[i].Title = t
			}
		}
	}
	live := 0
	for _, pg := range pages {
		if pg.RemovedAt.IsZero() {
			live++
		}
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		c, err := s.repo.Competitor(ctx, p.WorkspaceID, p.CompetitorID)
		if err != nil {
			return err
		}
		if err := s.repo.ReplaceCompetitorPages(ctx, c.ID(), pages); err != nil {
			return err
		}
		if err := s.repo.AddCompetitorChanges(ctx, changes); err != nil {
			return err
		}
		c.Checked(live, s.now())
		return s.repo.PutCompetitor(ctx, c)
	})
}
