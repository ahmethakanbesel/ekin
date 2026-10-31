// Package visibility models how answer engines talk about a workspace's
// brand: the prompts its buyers would ask, runs that ask each configured
// model every prompt, and each answer read for whether the brand is
// mentioned, where, which URLs it cites and which competitors appear.
package visibility

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
)

// Limits.
const (
	MaxPrompts      = 50
	MaxPromptRunes  = 500
	MaxModels       = 6
	MaxCompetitors  = 10
	MaxAliases      = 5
	MaxNameRunes    = 80
	DefaultCap      = 500 // calls a month
	MaxCap          = 20000
	AnswerRetention = 90 * 24 * time.Hour
)

// Schedule is how often a workspace is checked by itself.
type Schedule string

const (
	ScheduleOff    Schedule = "off"
	ScheduleWeekly Schedule = "weekly"
	ScheduleDaily  Schedule = "daily"
)

func (s Schedule) every() time.Duration {
	switch s {
	case ScheduleDaily:
		return 24 * time.Hour
	case ScheduleWeekly:
		return 7 * 24 * time.Hour
	}
	return 0
}

// Brand is a name to look for in answers, and its site's domain to look
// for in citations.
type Brand struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
	Domain  string   `json:"domain,omitempty"` // example.com
}

var modelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._:-]*$`)

// ParseDomain reads a domain or a URL as a bare lower-case host without
// www, or "" when s is blank.
func ParseDomain(s string) (string, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return "", nil
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" || !strings.Contains(u.Hostname(), ".") {
		return "", apperror.New(apperror.ValidationFailed, "a domain looks like example.com")
	}
	return strings.TrimPrefix(u.Hostname(), "www."), nil
}

func (b Brand) parse(what string) (Brand, error) {
	b.Name = strings.Join(strings.Fields(b.Name), " ")
	if b.Name == "" {
		return b, apperror.New(apperror.ValidationFailed, what+" needs a name")
	}
	if utf8.RuneCountInString(b.Name) > MaxNameRunes {
		return b, apperror.New(apperror.ValidationFailed, what+"'s name is too long")
	}
	var aliases []string
	for _, a := range b.Aliases {
		a = strings.Join(strings.Fields(a), " ")
		if a != "" && !strings.EqualFold(a, b.Name) && !slices.ContainsFunc(aliases, func(x string) bool { return strings.EqualFold(x, a) }) {
			aliases = append(aliases, a)
		}
	}
	if len(aliases) > MaxAliases {
		return b, apperror.New(apperror.ValidationFailed, "at most 5 other names")
	}
	b.Aliases = aliases
	d, err := ParseDomain(b.Domain)
	if err != nil {
		return b, err
	}
	b.Domain = d
	return b, nil
}

// Settings are what a person sets for the checks.
type Settings struct {
	Brand       Brand
	Competitors []Brand
	Models      []string // OpenRouter model ids
	Schedule    Schedule
	// MonthlyCap is how many calls (prompt × model) a calendar month may
	// make; a run that would pass it is refused, one that reaches it
	// stops.
	MonthlyCap int
}

func (st Settings) parse() (Settings, error) {
	var err error
	if st.Brand, err = st.Brand.parse("the brand"); err != nil {
		return st, err
	}
	if len(st.Competitors) > MaxCompetitors {
		return st, apperror.New(apperror.ValidationFailed, "at most 10 competitors")
	}
	for i, c := range st.Competitors {
		if st.Competitors[i], err = c.parse("a competitor"); err != nil {
			return st, err
		}
	}
	if len(st.Models) == 0 || len(st.Models) > MaxModels {
		return st, apperror.New(apperror.ValidationFailed, "choose between 1 and 6 models")
	}
	var models []string
	for _, m := range st.Models {
		m = strings.TrimSpace(strings.ToLower(m))
		if !modelPattern.MatchString(m) {
			return st, apperror.New(apperror.ValidationFailed, "a model is an OpenRouter id such as perplexity/sonar")
		}
		if !slices.Contains(models, m) {
			models = append(models, m)
		}
	}
	st.Models = models
	switch st.Schedule {
	case "":
		st.Schedule = ScheduleWeekly
	case ScheduleOff, ScheduleWeekly, ScheduleDaily:
	default:
		return st, apperror.New(apperror.ValidationFailed, "schedule must be off, weekly or daily")
	}
	if st.MonthlyCap == 0 {
		st.MonthlyCap = DefaultCap
	}
	if st.MonthlyCap < 1 || st.MonthlyCap > MaxCap {
		return st, apperror.New(apperror.ValidationFailed, "the monthly cap must be between 1 and 20000 calls")
	}
	return st, nil
}

// Profile is a workspace's visibility settings and schedule.
type Profile struct {
	workspaceID uuid.UUID
	settings    Settings
	nextRunAt   time.Time
	updatedAt   time.Time
}

// NewProfile sets a workspace's checks up; the first scheduled run is
// one interval away.
func NewProfile(ws uuid.UUID, st Settings, now time.Time) (*Profile, error) {
	if ws == uuid.Nil {
		return nil, apperror.New(apperror.ValidationFailed, "workspace is required")
	}
	st, err := st.parse()
	if err != nil {
		return nil, err
	}
	p := &Profile{workspaceID: ws, settings: st, updatedAt: timeutil.Normalize(now)}
	p.Ran(now)
	return p, nil
}

// Update changes the settings; a changed schedule counts from now.
func (p *Profile) Update(st Settings, now time.Time) error {
	st, err := st.parse()
	if err != nil {
		return err
	}
	changed := st.Schedule != p.settings.Schedule
	p.settings, p.updatedAt = st, timeutil.Normalize(now)
	if changed {
		p.Ran(now)
	}
	return nil
}

// Ran records that a run started at now.
func (p *Profile) Ran(now time.Time) {
	if every := p.settings.Schedule.every(); every > 0 {
		p.nextRunAt = timeutil.Normalize(now).Add(every)
	} else {
		p.nextRunAt = time.Time{}
	}
}

// Due reports whether a scheduled run is due at now.
func (p *Profile) Due(now time.Time) bool { return !p.nextRunAt.IsZero() && !now.Before(p.nextRunAt) }

func (p *Profile) WorkspaceID() uuid.UUID { return p.workspaceID }
func (p *Profile) Settings() Settings     { return p.settings }
func (p *Profile) NextRunAt() time.Time   { return p.nextRunAt }
func (p *Profile) UpdatedAt() time.Time   { return p.updatedAt }

// ProfileSnapshot is a Profile's stored form.
type ProfileSnapshot struct {
	WorkspaceID          uuid.UUID
	Settings             Settings
	NextRunAt, UpdatedAt time.Time
}

func (p *Profile) Snapshot() ProfileSnapshot {
	return ProfileSnapshot{WorkspaceID: p.workspaceID, Settings: p.settings, NextRunAt: p.nextRunAt, UpdatedAt: p.updatedAt}
}

// RehydrateProfile rebuilds a stored Profile.
func RehydrateProfile(sn ProfileSnapshot) (*Profile, error) {
	st, err := sn.Settings.parse()
	if err != nil || sn.WorkspaceID == uuid.Nil {
		return nil, apperror.New(apperror.InternalError, "stored visibility settings are invalid")
	}
	return &Profile{workspaceID: sn.WorkspaceID, settings: st, nextRunAt: timeutil.Normalize(sn.NextRunAt),
		updatedAt: timeutil.Normalize(sn.UpdatedAt)}, nil
}

// PromptSource says who wrote a prompt.
type PromptSource string

const (
	SourcePerson PromptSource = "person"
	SourceAgent  PromptSource = "agent"
)

// Prompt is a question buyers would ask an assistant.
type Prompt struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	Text        string
	Source      PromptSource
	Active      bool
	// CoveredBy is the latest story made to answer it (visibility
	// pipelines); Nil when none.
	CoveredBy uuid.UUID
	CoveredAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ParsePromptText checks a prompt's text.
func ParsePromptText(s string) (string, error) {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "", apperror.New(apperror.ValidationFailed, "a prompt is required")
	}
	if utf8.RuneCountInString(s) > MaxPromptRunes {
		return "", apperror.New(apperror.ValidationFailed, "a prompt holds at most 500 characters")
	}
	return s, nil
}

// RunStatus is where a run stands.
type RunStatus string

const (
	RunQueued  RunStatus = "queued"
	RunRunning RunStatus = "running"
	RunDone    RunStatus = "done"
	RunFailed  RunStatus = "failed"
	// RunCapped stopped when the month's cap was reached.
	RunCapped RunStatus = "capped"
)

// Run is one check: every active prompt asked of every model.
type Run struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	Status      RunStatus
	Trigger     string // manual | schedule
	Planned     int    // calls it set out to make
	Done        int    // calls answered
	Failed      int    // calls that failed
	Cost        float64
	Failure     string
	CreatedAt   time.Time
	StartedAt   time.Time
	FinishedAt  time.Time
}

// Finished reports whether the run ended.
func (r Run) Finished() bool {
	return r.Status == RunDone || r.Status == RunFailed || r.Status == RunCapped
}

// Allow reports whether a run of planned calls fits the month: used is
// the calls made this month so far.
func Allow(planned, used, monthlyCap int) error {
	if planned <= 0 {
		return apperror.New(apperror.ValidationFailed, "add at least one active prompt first")
	}
	if left := monthlyCap - used; planned > left {
		return apperror.New(apperror.ValidationFailed,
			"this run needs "+itoa(planned)+" calls but only "+itoa(max(left, 0))+" are left of this month's "+itoa(monthlyCap))
	}
	return nil
}

// MonthStart is the first instant of t's calendar month, UTC.
func MonthStart(t time.Time) time.Time {
	y, m, _ := t.UTC().Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
}

func itoa(n int) string { return strconv.Itoa(n) }

// Answer is one model's reply to one prompt in a run, as read.
type Answer struct {
	ID       uuid.UUID
	RunID    uuid.UUID
	PromptID uuid.UUID
	Model    string
	// Text is the reply; emptied after AnswerRetention, while what was
	// read from it is kept.
	Text      string
	Citations []string
	Analysis
	Error     string // set when the call failed
	CreatedAt time.Time
}

// Analysis is what an answer says about the brand and its competitors.
type Analysis struct {
	Mentioned bool `json:"mentioned"`
	// Position is the brand's place among the brands the answer names,
	// in order of first mention (1 is first); 0 when not named.
	Position int  `json:"position"`
	Cited    bool `json:"cited"` // a citation is on the brand's domain
	// Competitors are the competitors named, in order of first mention;
	// CompetitorsCited those whose domain a citation is on.
	Competitors      []string `json:"competitors,omitempty"`
	CompetitorsCited []string `json:"competitorsCited,omitempty"`
}

// Analyze reads text and citations for brand and competitors: a name
// counts when it stands as a whole word (any case), the domain when it
// appears in the text or a citation's host is it or under it.
func Analyze(text string, citations []string, brand Brand, competitors []Brand) Analysis {
	var a Analysis
	type hit struct {
		name string
		at   int
	}
	var hits []hit
	if at := firstMention(text, brand); at >= 0 {
		a.Mentioned = true
		hits = append(hits, hit{"", at})
	}
	a.Cited = citesDomain(citations, brand.Domain)
	for _, c := range competitors {
		if at := firstMention(text, c); at >= 0 {
			hits = append(hits, hit{c.Name, at})
		}
		if citesDomain(citations, c.Domain) {
			a.CompetitorsCited = append(a.CompetitorsCited, c.Name)
		}
	}
	slices.SortStableFunc(hits, func(x, y hit) int { return x.at - y.at })
	for i, h := range hits {
		if h.name == "" {
			a.Position = i + 1
		} else {
			a.Competitors = append(a.Competitors, h.name)
		}
	}
	return a
}

// firstMention is the byte offset of b's earliest name or domain in
// text, or -1.
func firstMention(text string, b Brand) int {
	first := -1
	for _, name := range append([]string{b.Name, b.Domain}, b.Aliases...) {
		if name == "" {
			continue
		}
		re := regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(` + regexp.QuoteMeta(name) + `)(?:$|[^\p{L}\p{N}])`)
		if loc := re.FindStringSubmatchIndex(text); loc != nil && (first < 0 || loc[2] < first) {
			first = loc[2]
		}
	}
	return first
}

func citesDomain(citations []string, domain string) bool {
	if domain == "" {
		return false
	}
	for _, c := range citations {
		u, err := url.Parse(c)
		if err != nil {
			continue
		}
		h := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
		if h == domain || strings.HasSuffix(h, "."+domain) {
			return true
		}
	}
	return false
}
