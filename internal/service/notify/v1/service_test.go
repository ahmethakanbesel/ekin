package notify_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

	notifyrepo "kurgu/internal/repository/notify/v1"
	notifysvc "kurgu/internal/service/notify/v1"
	productionsvc "kurgu/internal/service/production/v1"
	"kurgu/pkg/id"
)

type dir struct{}

func (dir) DigestRecipients(context.Context, uuid.UUID) ([]notifysvc.Recipient, error) {
	return []notifysvc.Recipient{{Email: "a@example.com"}, {Email: "b@example.com"}}, nil
}
func (dir) WorkspaceName(context.Context, uuid.UUID) (string, error) { return "acme", nil }

type mailer struct {
	sent []string
	fail bool
}

func (m *mailer) Enabled() bool { return true }
func (m *mailer) Send(to, subject, body string) error {
	if m.fail {
		return errors.New("relay down")
	}
	m.sent = append(m.sent, to+"|"+subject+"|"+body)
	return nil
}

func TestDigest(t *testing.T) {
	ctx := context.Background()
	m := &mailer{}
	svc := notifysvc.NewService(notifyrepo.NewInMemory(), dir{}, m, "https://kurgu.example.com", slog.New(slog.DiscardHandler))
	ws, story := id.New(), id.New()
	for _, e := range []productionsvc.Event{
		{WorkspaceID: ws, StoryID: story, Kind: "review", Title: "Ruminasyon", Detail: "video revision 1 is ready for review"},
		{WorkspaceID: ws, StoryID: id.New(), Kind: "failed", Title: "Uyku", Detail: "video failed: chrome"},
		{WorkspaceID: ws, Kind: "run", Title: "Shorts", Detail: "2 new stories"},
	} {
		if err := svc.Notify(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	m.fail = true
	if _, err := svc.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	m.fail = false
	n, err := svc.Flush(ctx)
	if err != nil || n != 2 || len(m.sent) != 2 {
		t.Fatalf("flush = %d %v %d", n, err, len(m.sent))
	}
	msg := m.sent[0]
	for _, want := range []string{"[acme] 1 ready for review, 1 failed", "Ruminasyon", "https://kurgu.example.com/w/" + ws.String() + "/stories/" + story.String()} {
		if !strings.Contains(msg, want) {
			t.Errorf("digest lacks %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "2 new stories") {
		t.Error("run summaries are feed-only")
	}
	if n, _ := svc.Flush(ctx); n != 0 {
		t.Fatalf("sent twice: %d", n)
	}
	feed, _ := svc.Feed(ctx, ws, 10)
	if len(feed) != 3 || feed[0].Kind != "run" {
		t.Fatalf("feed = %+v", feed)
	}
}
