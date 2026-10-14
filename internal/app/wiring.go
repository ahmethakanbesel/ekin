package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/adapter/claude"
	"kurgu/internal/adapter/openrouter"
	"kurgu/internal/adapter/remote"
	"kurgu/internal/adapter/render"
	"kurgu/internal/adapter/smtp"
	"kurgu/internal/config"
	identitydomain "kurgu/internal/domain/identity/v1"
	settingsdomain "kurgu/internal/domain/settings/v1"
	storydomain "kurgu/internal/domain/story/v1"
	storyrepo "kurgu/internal/repository/story/v1"
	identitysvc "kurgu/internal/service/identity/v1"
	jobsvc "kurgu/internal/service/job/v1"
	notifysvc "kurgu/internal/service/notify/v1"
	productionsvc "kurgu/internal/service/production/v1"
	settingssvc "kurgu/internal/service/settings/v1"
	workspacesvc "kurgu/internal/service/workspace/v1"
	systemhandler "kurgu/internal/transport/http/system/v1"
	remoteworker "kurgu/internal/transport/job/remote/v1"
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

// resetChecks drops the cached checks (after a credentials change).
func resetChecks() {
	checksMu.Lock()
	defer checksMu.Unlock()
	lastChecks = nil
}

// checks reports what this instance can do, cached for a minute.
func (a *App) checks(ctx context.Context) []systemhandler.Check {
	checksMu.Lock()
	defer checksMu.Unlock()
	if time.Since(checksAt) < time.Minute && lastChecks != nil {
		return lastChecks
	}
	p := a.cfg.Production
	creds := a.settings.Credentials(ctx)
	var out []systemhandler.Check
	add := func(name string, ok bool, detail string) {
		out = append(out, systemhandler.Check{Name: name, OK: ok, Detail: detail})
	}
	tool := func(name, bin string, args ...string) {
		ok, detail := probeTool(ctx, bin, args...)
		add(name, ok, detail)
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
	add("Stock media (Pexels)", creds.PexelsAPIKey != "", map[bool]string{true: "API key set", false: "no Pexels key; compositions use no stock footage"}[creds.PexelsAPIKey != ""])
	smtpOK := creds.SMTPHost != "" && creds.SMTPFrom != ""
	add("Email (SMTP)", smtpOK, map[bool]string{true: creds.SMTPHost, false: "SMTP host / from not set; digests stay in the app"}[smtpOK])
	yt := creds.YouTubeClientID != "" && creds.YouTubeClientSecret != ""
	redirect := "redirect URI " + a.cfg.PublicURL + "/api/v1/oauth/youtube/callback"
	add("YouTube publishing", yt, map[bool]string{true: "OAuth client set; " + redirect, false: "OAuth client ID / secret not set; register the " + redirect}[yt])
	add("Duplicate checks (Jev)", creds.OpenRouterAPIKey != "", map[bool]string{true: creds.DecisionModel, false: "no OpenRouter key; title similarity only"}[creds.OpenRouterAPIKey != ""])
	checksAt, lastChecks = time.Now(), out
	return out
}

// WorkRemote runs a hybrid worker: it claims agent and render jobs from
// the server at KURGU_REMOTE_URL and runs them on this machine. It opens
// no database.
func WorkRemote(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	client := &remote.Client{BaseURL: cfg.Remote.URL, Token: cfg.Remote.Token, Owner: cfg.Worker.Owner}
	prod, err := newProduction(cfg, nil, productionsvc.Deps{Log: log.With(slog.String("component", "production"))})
	if err != nil {
		return err
	}
	w := cfg.Worker
	runner := jobsvc.NewRunner(client, jobsvc.RunnerConfig{
		Owner: w.Owner,
		Throttle: jobsvc.Throttle{
			Concurrency: w.Concurrency, HeavyExclusive: w.HeavyExclusive, Cooldown: w.Cooldown, Paused: w.Paused,
		},
		Lease: w.Lease,
	}, log.With(slog.String("component", "remote-worker")))
	remoteworker.Register(runner, client, prod)
	log.InfoContext(ctx, "remote worker started", slog.String("server", cfg.Remote.URL), slog.String("owner", w.Owner))
	return runner.Run(ctx)
}

// probeTool runs bin with args and returns its first output line. It is
// detached from the caller's context: the result is cached for every
// later request, so a browser that gave up on one request must not turn
// working tools into "context canceled" for a minute.
func probeTool(ctx context.Context, bin string, args ...string) (bool, string) {
	path, err := exec.LookPath(bin)
	if err != nil {
		return false, bin + " not found on PATH"
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	v, err := exec.CommandContext(cctx, path, args...).CombinedOutput() //nolint:gosec // fixed tools
	if err != nil {
		return false, fmt.Sprintf("%s: %v", path, err)
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(v)), "\n")
	return true, line
}

// envCredentials are the KURGU_* values the settings page overrides.
// credentialDefaults are the values config falls back to when a variable
// is unset; envCredentials leaves those out so they are not reported as
// coming from the environment.
func credentialDefaults() settingsdomain.Credentials {
	return settingsdomain.Credentials{DecisionModel: config.DefaultDecisionModel, SMTPTLS: config.DefaultSMTPTLS}
}

func envCredentials(cfg config.Config) settingsdomain.Credentials {
	port := ""
	if cfg.Notify.SMTPPort > 0 {
		port = strconv.Itoa(cfg.Notify.SMTPPort)
	}
	c := settingsdomain.Credentials{
		ClaudeOAuthToken: os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"), ClaudeModel: cfg.Production.ClaudeModel,
		PexelsAPIKey: cfg.Production.PexelsKey, OpenRouterAPIKey: cfg.Notify.OpenRouterKey, DecisionModel: cfg.Notify.DecisionModel,
		SMTPHost: cfg.Notify.SMTPHost, SMTPPort: port, SMTPUsername: cfg.Notify.SMTPUser, SMTPPassword: cfg.Notify.SMTPPassword,
		SMTPFrom: cfg.Notify.SMTPFrom, SMTPTLS: cfg.Notify.SMTPTLS,
		YouTubeClientID: cfg.Publishing.YouTubeClientID, YouTubeClientSecret: cfg.Publishing.YouTubeClientSecret,
	}
	if _, ok := os.LookupEnv("KURGU_DECISION_MODEL"); !ok {
		c.DecisionModel = ""
	}
	if _, ok := os.LookupEnv("KURGU_SMTP_TLS"); !ok {
		c.SMTPTLS = ""
	}
	return c
}

// dynamicAgent runs Claude Code with the current model and token.
type dynamicAgent struct {
	settings *settingssvc.Service
	base     claude.Runner
}

func (a dynamicAgent) Run(ctx context.Context, req claude.Request) (claude.Result, error) {
	c := a.settings.Credentials(ctx)
	r := a.base
	r.Model = c.ClaudeModel
	if c.ClaudeOAuthToken != "" {
		r.Env = append(append([]string(nil), r.Env...), "CLAUDE_CODE_OAUTH_TOKEN="+c.ClaudeOAuthToken)
	}
	return r.Run(ctx, req)
}

// dynamicDedupe asks Jev with the current OpenRouter key.
type dynamicDedupe struct{ settings *settingssvc.Service }

func (d dynamicDedupe) Duplicate(ctx context.Context, idea storydomain.Idea, similar []storyrepo.Match) (bool, string, error) {
	c := d.settings.Credentials(ctx)
	return openrouter.Dedupe{Client: &openrouter.Client{APIKey: c.OpenRouterAPIKey, Model: c.DecisionModel}}.Duplicate(ctx, idea, similar)
}

// dynamicMailer sends through the current SMTP settings.
type dynamicMailer struct{ settings *settingssvc.Service }

func (m dynamicMailer) mailer() *smtp.Mailer {
	c := m.settings.Credentials(context.Background())
	return smtp.New(smtp.Config{Host: c.SMTPHost, Port: c.Port(), Username: c.SMTPUsername, Password: c.SMTPPassword, From: c.SMTPFrom, TLS: c.SMTPTLS})
}

func (m dynamicMailer) Enabled() bool { return m.mailer().Enabled() }
func (m dynamicMailer) Send(to, subject, body string) error {
	return m.mailer().Send(to, subject, body)
}
