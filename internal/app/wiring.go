package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/adapter/render"
	identitydomain "kurgu/internal/domain/identity/v1"
	identitysvc "kurgu/internal/service/identity/v1"
	notifysvc "kurgu/internal/service/notify/v1"
	workspacesvc "kurgu/internal/service/workspace/v1"
	systemhandler "kurgu/internal/transport/http/system/v1"
)

// directory answers the notify service's questions about people.
type directory struct {
	identity   *identitysvc.Service
	workspaces *workspacesvc.Service
}

// DigestRecipients are owners and editors who want email.
func (d directory) DigestRecipients(ctx context.Context, workspaceID uuid.UUID) ([]notifysvc.Recipient, error) {
	members, err := d.identity.Members(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	var out []notifysvc.Recipient
	for _, m := range members {
		if m.User.EmailNotifications() && m.Membership.Role() != identitydomain.RoleViewer {
			out = append(out, notifysvc.Recipient{Email: string(m.User.Email()), Name: m.User.Name()})
		}
	}
	return out, nil
}

func (d directory) WorkspaceName(ctx context.Context, workspaceID uuid.UUID) (string, error) {
	w, err := d.workspaces.Get(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	return w.Name(), nil
}

// inviter emails invitation links through the notify service.
type inviter struct {
	notify     *notifysvc.Service
	workspaces *workspacesvc.Service
}

func (i inviter) InviteEmail(ctx context.Context, workspaceID uuid.UUID, to, from, link string) (bool, error) {
	w, err := i.workspaces.Get(ctx, workspaceID)
	if err != nil {
		return false, err
	}
	return i.notify.Invite(ctx, to, w.Name(), from, link)
}

// flushNotifications sends email digests every KURGU_NOTIFY_EVERY.
func (a *App) flushNotifications(ctx context.Context) {
	t := time.NewTicker(a.cfg.Notify.Every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if n, err := a.notify.Flush(ctx); err != nil && ctx.Err() == nil {
			a.log.ErrorContext(ctx, "notification flush failed", slog.Any("error", err))
		} else if n > 0 {
			a.log.InfoContext(ctx, "digests sent", slog.Int("emails", n))
		}
	}
}

var (
	checksMu   sync.Mutex
	checksAt   time.Time
	lastChecks []systemhandler.Check
)

// checks reports what this instance can do, cached for a minute.
func (a *App) checks(ctx context.Context) []systemhandler.Check {
	checksMu.Lock()
	defer checksMu.Unlock()
	if time.Since(checksAt) < time.Minute && lastChecks != nil {
		return lastChecks
	}
	p := a.cfg.Production
	var out []systemhandler.Check
	add := func(name string, ok bool, detail string) {
		out = append(out, systemhandler.Check{Name: name, OK: ok, Detail: detail})
	}
	tool := func(name, bin string, args ...string) {
		path, err := exec.LookPath(bin)
		if err != nil {
			add(name, false, bin+" not found on PATH")
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		v, err := exec.CommandContext(cctx, path, args...).CombinedOutput() //nolint:gosec // fixed tools
		if err != nil {
			add(name, false, fmt.Sprintf("%s: %v", path, err))
			return
		}
		line, _, _ := strings.Cut(strings.TrimSpace(string(v)), "\n")
		add(name, true, line)
	}
	tool("Claude Code", p.ClaudeBin, "--version")
	tool("ffmpeg", p.FFmpeg, "-version")
	tool("uv (speech)", p.UV, "--version")
	tool("pdftotext", a.cfg.Tools.PDFToText, "-v")
	chrome := p.Chrome
	if chrome == "" {
		chrome = render.FindChrome()
	}
	add("Chrome", chrome != "", map[bool]string{true: chrome, false: "not found; set KURGU_CHROME"}[chrome != ""])
	_, okErr := os.Stat(filepath.Join(p.SpeechDir, "okur_say.py"))
	add("Speech scripts", okErr == nil, p.SpeechDir)
	add("Stock media (Pexels)", p.PexelsKey != "", map[bool]string{true: "API key set", false: "KURGU_PEXELS_API_KEY not set; compositions use no stock footage"}[p.PexelsKey != ""])
	add("Email (SMTP)", a.cfg.Notify.SMTPHost != "" && a.cfg.Notify.SMTPFrom != "", map[bool]string{true: a.cfg.Notify.SMTPHost, false: "KURGU_SMTP_HOST / KURGU_SMTP_FROM not set; digests stay in the app"}[a.cfg.Notify.SMTPHost != ""])
	yt := a.cfg.Publishing.YouTubeClientID != "" && a.cfg.Publishing.YouTubeClientSecret != ""
	add("YouTube publishing", yt, map[bool]string{true: "OAuth client set; redirect URI " + a.cfg.PublicURL + "/api/v1/oauth/youtube/callback", false: "KURGU_YOUTUBE_CLIENT_ID / KURGU_YOUTUBE_CLIENT_SECRET not set"}[yt])
	add("Duplicate checks (Jev)", a.cfg.Notify.OpenRouterKey != "", map[bool]string{true: a.cfg.Notify.DecisionModel, false: "KURGU_OPENROUTER_API_KEY not set; title similarity only"}[a.cfg.Notify.OpenRouterKey != ""])
	checksAt, lastChecks = time.Now(), out
	return out
}
