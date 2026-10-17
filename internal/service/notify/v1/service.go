// Package notify records what needs people's attention (a feed per
// workspace) and emails digests: one message per person per flush,
// listing everything that became ready, failed or went live.
package notify

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	notifyrepo "kurgu/internal/repository/notify/v1"
	productionsvc "kurgu/internal/service/production/v1"
	"kurgu/pkg/id"
)

// Recipient is someone who gets digests.
type Recipient struct {
	Email string
	Name  string
}

// Directory answers who to email and what a workspace is called.
type Directory interface {
	DigestRecipients(ctx context.Context, workspaceID uuid.UUID) ([]Recipient, error)
	WorkspaceName(ctx context.Context, workspaceID uuid.UUID) (string, error)
}

// Mailer sends email (internal/adapter/smtp).
type Mailer interface {
	Enabled() bool
	Send(to, subject, body string) error
}

// Notification is a feed entry.
type Notification = notifyrepo.Notification

// Service records and sends notifications.
type Service struct {
	repo      notifyrepo.Repository
	dir       Directory
	mailer    Mailer
	publicURL string
	log       *slog.Logger
	now       func() time.Time
}

// NewService returns a Service.
func NewService(repo notifyrepo.Repository, dir Directory, mailer Mailer, publicURL string, log *slog.Logger) *Service {
	return &Service{repo: repo, dir: dir, mailer: mailer, publicURL: strings.TrimRight(publicURL, "/"), log: log, now: time.Now}
}

// WithClock replaces the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Notify implements productionsvc.Notifier: it records e in the feed.
// Run summaries ("3 new stories in production") stay in the feed only.
func (s *Service) Notify(ctx context.Context, e productionsvc.Event) error {
	return s.repo.Add(ctx, notifyrepo.Notification{
		ID: id.New(), WorkspaceID: e.WorkspaceID, StoryID: e.StoryID, Kind: e.Kind,
		Title: e.Title, Detail: e.Detail, CreatedAt: s.now(),
	})
}

// FeedFilter narrows Feed and picks its page.
type FeedFilter = notifyrepo.FeedFilter

// Feed returns a page of a workspace's notifications, newest first, and
// the cursor of the next page ("" after the last).
func (s *Service) Feed(ctx context.Context, workspaceID uuid.UUID, f FeedFilter) ([]Notification, string, error) {
	return s.repo.Feed(ctx, workspaceID, f)
}

// Flush emails one digest per workspace member for everything recorded
// since the last flush, and returns how many messages it sent. Without a
// mailer it only marks entries as handled.
func (s *Service) Flush(ctx context.Context) (int, error) {
	pending, err := s.repo.Unsent(ctx, 500)
	if err != nil || len(pending) == 0 {
		return 0, err
	}
	byWS := map[uuid.UUID][]Notification{}
	var order []uuid.UUID
	for _, n := range pending {
		if _, ok := byWS[n.WorkspaceID]; !ok {
			order = append(order, n.WorkspaceID)
		}
		byWS[n.WorkspaceID] = append(byWS[n.WorkspaceID], n)
	}
	sent := 0
	for _, ws := range order {
		items := byWS[ws]
		ids := make([]uuid.UUID, 0, len(items))
		for _, n := range items {
			ids = append(ids, n.ID)
		}
		emailable := slices.DeleteFunc(slices.Clone(items), func(n Notification) bool { return n.Kind == "run" })
		if s.mailer != nil && s.mailer.Enabled() && len(emailable) > 0 {
			n, err := s.sendDigest(ctx, ws, emailable)
			sent += n
			if err != nil {
				s.log.WarnContext(ctx, "digest not sent; will retry", slog.Any("error", err))
				continue
			}
		}
		if err := s.repo.MarkEmailed(ctx, ids, s.now()); err != nil {
			return sent, err
		}
	}
	return sent, nil
}

func (s *Service) sendDigest(ctx context.Context, ws uuid.UUID, items []Notification) (int, error) {
	recipients, err := s.dir.DigestRecipients(ctx, ws)
	if err != nil || len(recipients) == 0 {
		return 0, err
	}
	name, err := s.dir.WorkspaceName(ctx, ws)
	if err != nil {
		return 0, err
	}
	counts := map[string]int{}
	for _, n := range items {
		counts[n.Kind]++
	}
	var parts []string
	for _, k := range []string{"review", "failed", "published"} {
		if c := counts[k]; c > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c, map[string]string{"review": "ready for review", "failed": "failed", "published": "published"}[k]))
		}
	}
	subject := fmt.Sprintf("[%s] %s", name, strings.Join(parts, ", "))
	var b strings.Builder
	for _, k := range []string{"review", "failed", "published"} {
		first := true
		for _, n := range items {
			if n.Kind != k {
				continue
			}
			if first {
				b.WriteString(map[string]string{"review": "Ready for review", "failed": "Failed", "published": "Published"}[k] + "\n\n")
				first = false
			}
			fmt.Fprintf(&b, "• %s\n  %s\n", n.Title, n.Detail)
			if n.StoryID != uuid.Nil && s.publicURL != "" {
				fmt.Fprintf(&b, "  %s/w/%s/stories/%s\n", s.publicURL, ws, n.StoryID)
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("— Kurgu\nTurn these emails off on your account page.\n")
	sent := 0
	for _, r := range recipients {
		if err := s.mailer.Send(r.Email, subject, b.String()); err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}

// Invite emails an invitation link, when mail is configured. It reports
// whether it sent anything.
func (s *Service) Invite(ctx context.Context, to, workspaceName, inviter, link string) (bool, error) {
	if s.mailer == nil || !s.mailer.Enabled() {
		return false, nil
	}
	body := fmt.Sprintf("%s invited you to the %s workspace on Kurgu.\n\nAccept within 7 days:\n%s\n", inviter, workspaceName, link)
	if err := s.mailer.Send(to, "Join "+workspaceName+" on Kurgu", body); err != nil {
		s.log.WarnContext(ctx, "invitation email failed", slog.Any("error", err))
		return false, err
	}
	return true, nil
}
