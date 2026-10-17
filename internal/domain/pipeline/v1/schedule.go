package pipeline

import (
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"ekin/internal/apperror"
)

// Schedule limits.
const (
	MaxBatch = 20
	// MinInterval is the shortest gap allowed between two runs, so a typo
	// like "* * * * *" cannot queue a video every minute.
	MinInterval = time.Hour
	// intervalProbe is how many consecutive firings ParseSchedule checks
	// against MinInterval. Cron gaps repeat within a year, but a full year
	// of an hourly schedule is 8760 firings; a few weeks of daily runs and
	// every gap shape of the common expressions fit in this many.
	intervalProbe = 400
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// Schedule fires a pipeline run on a cron expression evaluated in a
// timezone, producing batch items per run.
type Schedule struct {
	expr     string
	location *time.Location
	batch    int
	spec     cron.Schedule
}

// ParseSchedule parses a five-field cron expression (or a descriptor such
// as "@daily") in loc. The timezone is a separate value, so an inline
// CRON_TZ= / TZ= prefix is rejected.
func ParseSchedule(expr string, loc *time.Location, batch int) (Schedule, error) {
	expr = strings.Join(strings.Fields(expr), " ")
	if expr == "" {
		return Schedule{}, apperror.New(apperror.ValidationFailed, "cron expression is required")
	}
	if strings.Contains(expr, "TZ=") {
		return Schedule{}, apperror.New(apperror.ValidationFailed, "set the timezone separately, not inside the cron expression")
	}
	if strings.HasPrefix(expr, "@every") {
		return Schedule{}, apperror.New(apperror.ValidationFailed, "@every is not supported; use a cron expression")
	}
	if loc == nil {
		return Schedule{}, apperror.New(apperror.ValidationFailed, "schedule timezone is required")
	}
	if batch < 1 || batch > MaxBatch {
		return Schedule{}, apperror.New(apperror.ValidationFailed, "items per run must be between 1 and 20")
	}
	spec, err := cronParser.Parse(expr)
	if err != nil {
		return Schedule{}, apperror.Wrap(apperror.ValidationFailed, err, "invalid cron expression")
	}
	s := Schedule{expr: expr, location: loc, batch: batch, spec: spec}
	if err := s.checkInterval(); err != nil {
		return Schedule{}, err
	}
	return s, nil
}

func (s Schedule) checkInterval() error {
	t := time.Date(2026, 1, 1, 0, 0, 0, 0, s.location)
	prev := s.spec.Next(t)
	if prev.IsZero() {
		return apperror.New(apperror.ValidationFailed, "cron expression never fires")
	}
	for range intervalProbe {
		next := s.spec.Next(prev)
		if next.IsZero() {
			return nil
		}
		if next.Sub(prev) < MinInterval {
			return apperror.New(apperror.ValidationFailed, "runs must be at least one hour apart")
		}
		prev = next
	}
	return nil
}

// Next returns the first firing strictly after after, in UTC. Wall-clock
// times are evaluated in the schedule's timezone, so "0 2 * * *" stays at
// 02:00 local across daylight-saving changes. It returns the zero time
// for an expression that never fires again.
func (s Schedule) Next(after time.Time) time.Time {
	next := s.spec.Next(after.In(s.location))
	if next.IsZero() {
		return time.Time{}
	}
	return next.UTC()
}

func (s Schedule) Expr() string             { return s.expr }
func (s Schedule) Location() *time.Location { return s.location }
func (s Schedule) Batch() int               { return s.batch }

// Equal reports whether two schedules fire identically.
func (s Schedule) Equal(o Schedule) bool {
	return s.expr == o.expr && s.location.String() == o.location.String() && s.batch == o.batch
}
