package site

import (
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
)

// Severity is how much a finding matters.
type Severity string

const (
	SeverityError   Severity = "error"   // breaks something for visitors or search engines
	SeverityWarning Severity = "warning" // likely costs visibility
	SeverityNotice  Severity = "notice"  // worth a look
)

// Rank orders severities, most severe first.
func (s Severity) Rank() int {
	switch s {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	}
	return 2
}

// ParseSeverity parses a severity.
func ParseSeverity(s string) (Severity, error) {
	switch sv := Severity(s); sv {
	case SeverityError, SeverityWarning, SeverityNotice:
		return sv, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "severity must be error, warning or notice")
}

// FindingStatus is where a finding stands.
//
//	open → resolved (the next crawl no longer raises it) → open (it came back)
//	open ⇄ ignored (a person decided it does not matter here)
type FindingStatus string

const (
	FindingOpen     FindingStatus = "open"
	FindingResolved FindingStatus = "resolved"
	FindingIgnored  FindingStatus = "ignored"
)

// ParseFindingStatus parses a finding status.
func ParseFindingStatus(s string) (FindingStatus, error) {
	switch st := FindingStatus(s); st {
	case FindingOpen, FindingResolved, FindingIgnored:
		return st, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "status must be open, resolved or ignored")
}

// MaxEvidence is how many evidence lines a finding keeps.
const MaxEvidence = 20

// Raised is a finding as the audit raises it.
type Raised struct {
	Rule     Rule
	URL      string   // the page it is about; the site's address for site-wide rules
	Evidence []string // what shows it, for a person to check
}

// Key identifies a finding across crawls: the same rule on the same page.
func (r Raised) Key() string { return string(r.Rule) + " " + r.URL }

// Finding is a rule broken on a page, followed across crawls.
type Finding struct {
	id          uuid.UUID
	workspaceID uuid.UUID
	rule        Rule
	url         string
	evidence    []string
	status      FindingStatus
	crawlID     uuid.UUID // the crawl that last raised it
	firstSeen   time.Time
	lastSeen    time.Time
	resolvedAt  time.Time
	updatedAt   time.Time
}

func newFinding(id, ws, crawl uuid.UUID, r Raised, now time.Time) *Finding {
	return &Finding{id: id, workspaceID: ws, rule: r.Rule, url: r.URL, evidence: clip(r.Evidence), status: FindingOpen,
		crawlID: crawl, firstSeen: now, lastSeen: now, updatedAt: now}
}

func clip(ev []string) []string {
	if len(ev) > MaxEvidence {
		ev = ev[:MaxEvidence]
	}
	return slices.Clone(ev)
}

// Reconcile applies a finished crawl's audit to the workspace's findings
// and returns the ones it created or changed. A raised finding is
// created, kept open, or reopened after it was resolved; an ignored one
// stays ignored. An open finding the crawl no longer raises is resolved.
func Reconcile(existing []*Finding, raised []Raised, ws, crawl uuid.UUID, ids func() uuid.UUID, now time.Time) []*Finding {
	now = timeutil.Normalize(now)
	byKey := make(map[string]*Finding, len(existing))
	for _, f := range existing {
		byKey[f.Key()] = f
	}
	var changed []*Finding
	seen := map[string]bool{}
	for _, r := range raised {
		k := r.Key()
		if seen[k] {
			continue
		}
		seen[k] = true
		f, ok := byKey[k]
		if !ok {
			changed = append(changed, newFinding(ids(), ws, crawl, r, now))
			continue
		}
		f.evidence, f.crawlID, f.lastSeen, f.updatedAt = clip(r.Evidence), crawl, now, now
		if f.status == FindingResolved {
			f.status, f.resolvedAt = FindingOpen, time.Time{}
		}
		changed = append(changed, f)
	}
	for _, f := range existing {
		if !seen[f.Key()] && f.status == FindingOpen {
			f.status, f.resolvedAt, f.updatedAt = FindingResolved, now, now
			changed = append(changed, f)
		}
	}
	return changed
}

// SetStatus lets a person ignore an open finding or bring an ignored one
// back; resolved findings are the crawl's to reopen.
func (f *Finding) SetStatus(to FindingStatus, now time.Time) error {
	switch {
	case to == f.status:
		return nil
	case f.status == FindingOpen && to == FindingIgnored, f.status == FindingIgnored && to == FindingOpen:
		f.status, f.updatedAt = to, timeutil.Normalize(now)
		return nil
	}
	return apperror.New(apperror.InvalidTransition, "a "+string(f.status)+" finding cannot become "+string(to))
}

func (f *Finding) Key() string            { return Raised{Rule: f.rule, URL: f.url}.Key() }
func (f *Finding) ID() uuid.UUID          { return f.id }
func (f *Finding) WorkspaceID() uuid.UUID { return f.workspaceID }
func (f *Finding) Rule() Rule             { return f.rule }
func (f *Finding) Severity() Severity     { return f.rule.Severity() }
func (f *Finding) URL() string            { return f.url }
func (f *Finding) Evidence() []string     { return slices.Clone(f.evidence) }
func (f *Finding) Status() FindingStatus  { return f.status }
func (f *Finding) CrawlID() uuid.UUID     { return f.crawlID }
func (f *Finding) FirstSeen() time.Time   { return f.firstSeen }
func (f *Finding) LastSeen() time.Time    { return f.lastSeen }
func (f *Finding) ResolvedAt() time.Time  { return f.resolvedAt }
func (f *Finding) UpdatedAt() time.Time   { return f.updatedAt }

// FindingSnapshot is a Finding's stored form.
type FindingSnapshot struct {
	ID, WorkspaceID, CrawlID                   uuid.UUID
	Rule, URL, Status                          string
	Evidence                                   []string
	FirstSeen, LastSeen, ResolvedAt, UpdatedAt time.Time
}

func (f *Finding) Snapshot() FindingSnapshot {
	return FindingSnapshot{ID: f.id, WorkspaceID: f.workspaceID, CrawlID: f.crawlID, Rule: string(f.rule), URL: f.url,
		Status: string(f.status), Evidence: slices.Clone(f.evidence), FirstSeen: f.firstSeen, LastSeen: f.lastSeen,
		ResolvedAt: f.resolvedAt, UpdatedAt: f.updatedAt}
}

// RehydrateFinding rebuilds a stored Finding. A rule this version no
// longer knows is kept; it reads as a notice.
func RehydrateFinding(sn FindingSnapshot) (*Finding, error) {
	st, err := ParseFindingStatus(sn.Status)
	if err != nil || sn.ID == uuid.Nil || sn.WorkspaceID == uuid.Nil || sn.Rule == "" {
		return nil, apperror.New(apperror.InternalError, "stored finding is invalid")
	}
	return &Finding{id: sn.ID, workspaceID: sn.WorkspaceID, crawlID: sn.CrawlID, rule: Rule(sn.Rule), url: sn.URL,
		status: st, evidence: slices.Clone(sn.Evidence), firstSeen: timeutil.Normalize(sn.FirstSeen),
		lastSeen: timeutil.Normalize(sn.LastSeen), resolvedAt: timeutil.Normalize(sn.ResolvedAt),
		updatedAt: timeutil.Normalize(sn.UpdatedAt)}, nil
}
