package app

import (
	"context"
	"ekin/internal/adapter/elevenlabs"
	"ekin/internal/adapter/openseo"
	seosvc "ekin/internal/service/seo/v1"
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

	"ekin/internal/adapter/claude"
	"ekin/internal/adapter/cpulimit"
	"ekin/internal/adapter/openrouter"
	"ekin/internal/adapter/remote"
	"ekin/internal/adapter/render"
	"ekin/internal/adapter/smtp"
	"ekin/internal/config"
	identitydomain "ekin/internal/domain/identity/v1"
	settingsdomain "ekin/internal/domain/settings/v1"
	storydomain "ekin/internal/domain/story/v1"
	storyrepo "ekin/internal/repository/story/v1"
	identitysvc "ekin/internal/service/identity/v1"
	jobsvc "ekin/internal/service/job/v1"
	notifysvc "ekin/internal/service/notify/v1"
	productionsvc "ekin/internal/service/production/v1"
	settingssvc "ekin/internal/service/settings/v1"
	workspacesvc "ekin/internal/service/workspace/v1"
	systemhandler "ekin/internal/transport/http/system/v1"
	remoteworker "ekin/internal/transport/job/remote/v1"
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

// flushNotifications sends email digests every EKIN_NOTIFY_EVERY.
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
	tool("pdftohtml (PDF contents)", a.cfg.Tools.PDFToHTML, "-v")
	chrome := p.Chrome
	if chrome == "" {
		chrome = render.FindChrome()
	}
	add("Chrome", chrome != "", map[bool]string{true: chrome, false: "not found; set EKIN_CHROME"}[chrome != ""])
	_, okErr := os.Stat(filepath.Join(p.SpeechDir, "ema_say.py"))
	add("Speech scripts", okErr == nil, p.SpeechDir)
	add("CPU limit", cpulimit.Supported(), map[bool]string{
		true:  "available: job processes follow the host profile's CPU limit",
		false: "Linux only: the host profile's CPU limit has no effect on this OS",
	}[cpulimit.Supported()])
	add("Job priority", cpulimit.NiceSupported(), map[bool]string{
		true:  "available: job processes run at the host profile's priority (nice)",
		false: "not supported on this OS: the host profile's priority has no effect",
	}[cpulimit.NiceSupported()])
	add("Stock media (Pexels)", creds.PexelsAPIKey != "", map[bool]string{true: "API key set", false: "no Pexels key; compositions use no stock footage"}[creds.PexelsAPIKey != ""])
	smtpOK := creds.SMTPHost != "" && creds.SMTPFrom != ""
	add("Email (SMTP)", smtpOK, map[bool]string{true: creds.SMTPHost, false: "SMTP host / from not set; digests stay in the app"}[smtpOK])
	yt := creds.YouTubeClientID != "" && creds.YouTubeClientSecret != ""
	redirect := "redirect URI " + a.cfg.PublicURL + "/api/v1/oauth/youtube/callback"
	add("YouTube publishing", yt, map[bool]string{true: "OAuth client set; " + redirect, false: "OAuth client ID / secret not set; register the " + redirect}[yt])
	if creds.OpenSEOAPIKey == "" {
		add("OpenSEO", false, "no API key; SEO refreshes use the host's Claude Code OpenSEO plugin")
	} else {
		octx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		acct, err := (&openseo.Client{URL: creds.OpenSEOURL, APIKey: creds.OpenSEOAPIKey}).Whoami(octx)
		cancel()
		switch {
		case err != nil:
			add("OpenSEO", false, err.Error())
		case acct.CreditsRemaining != nil:
			add("OpenSEO", true, fmt.Sprintf("%s · %.0f credits left", acct.UserEmail, *acct.CreditsRemaining))
		default:
			add("OpenSEO", true, acct.UserEmail)
		}
	}
	if creds.ElevenLabsAPIKey == "" {
		add("ElevenLabs", false, "no API key; narration uses the local engines")
	} else {
		ectx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		el := &elevenlabs.Client{APIKey: creds.ElevenLabsAPIKey}
		sub, err := el.Subscription(ectx)
		if err != nil {
			// A key limited to text to speech cannot read usage; listing
			// models still proves it works.
			if _, merr := el.Models(ectx); merr == nil {
				add("ElevenLabs", true, "API key works (no permission to read usage)")
			} else {
				add("ElevenLabs", false, merr.Error())
			}
		} else {
			add("ElevenLabs", true, fmt.Sprintf("%s plan · %d of %d characters left", sub.Tier,
				max(0, sub.CharacterLimit-sub.CharacterCount), sub.CharacterLimit))
		}
		cancel()
	}
	add("Duplicate checks (Jev)", creds.OpenRouterAPIKey != "", map[bool]string{true: creds.DecisionModel, false: "no OpenRouter key; title similarity only"}[creds.OpenRouterAPIKey != ""])
	checksAt, lastChecks = time.Now(), out
	return out
}

// WorkRemote runs a hybrid worker: it claims agent and render jobs from
// the server at EKIN_REMOTE_URL and runs them on this machine. It opens
// no database.
func WorkRemote(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	client := &remote.Client{BaseURL: cfg.Remote.URL, Token: cfg.Remote.Token, Owner: cfg.Worker.Owner}
	// The host profile saved on the server (System, or the Jobs page)
	// wins over this machine's EKIN_* settings, and changes apply live.
	profile := remote.NewProfileWatcher(client, defaultProfile(cfg), 0, log.With(slog.String("component", "remote-profile")))
	profile.Refresh(ctx)
	go profile.Run(ctx)
	prod, err := newProduction(cfg, nil, profile.Profile, productionsvc.Deps{Log: log.With(slog.String("component", "production"))})
	limiter := &cpulimit.Limiter{
		Percent: func(ctx context.Context) int { return profile.Profile(ctx).CPULimit },
		Changed: profile.Changed, Log: log.With(slog.String("component", "cpulimit")),
	}
	go limiter.Run(ctx)
	nicer := &cpulimit.Nicer{
		Nice:    func(ctx context.Context) int { return profile.Profile(ctx).Nice },
		Changed: profile.Changed, Log: log.With(slog.String("component", "nice")),
	}
	go nicer.Run(ctx)
	if err != nil {
		return err
	}
	w := cfg.Worker
	runner := jobsvc.NewRunner(client, jobsvc.RunnerConfig{
		Owner:           w.Owner,
		ThrottleSource:  profile.Throttle,
		ThrottleChanged: profile.Changed,
		Lease:           w.Lease,
	}, log.With(slog.String("component", "remote-worker")))
	remoteworker.Register(runner, client, prod)
	log.InfoContext(ctx, "remote worker started", slog.String("server", cfg.Remote.URL), slog.String("owner", w.Owner),
		slog.String("profile", w.Profile))
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

// envCredentials are the EKIN_* values the settings page overrides.
// credentialDefaults are the values config falls back to when a variable
// is unset; envCredentials leaves those out so they are not reported as
// coming from the environment.
func credentialDefaults() settingsdomain.Credentials {
	return settingsdomain.Credentials{DecisionModel: config.DefaultDecisionModel, SMTPTLS: config.DefaultSMTPTLS, OpenSEOURL: openseo.DefaultURL}
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
		OpenSEOAPIKey: os.Getenv("EKIN_OPENSEO_API_KEY"), OpenSEOURL: os.Getenv("EKIN_OPENSEO_URL"),
		ElevenLabsAPIKey: cfg.Production.ElevenLabsKey,
	}
	if _, ok := os.LookupEnv("EKIN_DECISION_MODEL"); !ok {
		c.DecisionModel = ""
	}
	if _, ok := os.LookupEnv("EKIN_SMTP_TLS"); !ok {
		c.SMTPTLS = ""
	}
	return c
}

// openSEO returns a client for the current OpenSEO key, nil without one.
func openSEO(settings *settingssvc.Service) func(ctx context.Context) seosvc.Research {
	return func(ctx context.Context) seosvc.Research {
		c := settings.Credentials(ctx)
		if c.OpenSEOAPIKey == "" {
			return nil
		}
		return &openseo.Client{URL: c.OpenSEOURL, APIKey: c.OpenSEOAPIKey}
	}
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
