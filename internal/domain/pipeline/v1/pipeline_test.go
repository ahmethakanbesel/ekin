package pipeline_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	pipeline "kurgu/internal/domain/pipeline/v1"
)

// t0 is 2026-10-13 10:00 Istanbul time.
var t0 = time.Date(2026, 10, 13, 7, 0, 0, 0, time.UTC)

func nightly(t *testing.T, batch int) *pipeline.Schedule {
	t.Helper()
	s, err := pipeline.ParseSchedule("0 2 * * *", mustLoc(t, "Europe/Istanbul"), batch)
	if err != nil {
		t.Fatal(err)
	}
	return &s
}

func definition(t *testing.T, schedule *pipeline.Schedule) pipeline.Definition {
	t.Helper()
	video, err := pipeline.NewVideoSpec(pipeline.FormatShort, pipeline.Vertical, 45*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := pipeline.NewOutputs(&video, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	source, err := pipeline.NewPromptSource("Kaygı ve stres üzerine eğitici kısa videolar.")
	if err != nil {
		t.Fatal(err)
	}
	return pipeline.Definition{
		Name:     "Psikoloji shorts",
		Language: pipeline.Turkish,
		Outputs:  outputs,
		Source:   source,
		Schedule: schedule,
	}
}

func newPipeline(t *testing.T, schedule *pipeline.Schedule) *pipeline.Pipeline {
	t.Helper()
	p, err := pipeline.New(uuid.New(), uuid.New(), definition(t, schedule), t0)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNewPlansFirstRun(t *testing.T) {
	p := newPipeline(t, nightly(t, 5))
	want := time.Date(2026, 10, 13, 23, 0, 0, 0, time.UTC) // 02:00 Istanbul on the 14th
	if !p.NextRunAt().Equal(want) {
		t.Fatalf("next run = %v, want %v", p.NextRunAt(), want)
	}
	if manual := newPipeline(t, nil); !manual.NextRunAt().IsZero() {
		t.Fatalf("manual pipeline has a next run: %v", manual.NextRunAt())
	}
}

func TestDefinitionRules(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*pipeline.Definition)
	}{
		{"blank name", func(d *pipeline.Definition) { d.Name = "  " }},
		{"unknown language", func(d *pipeline.Definition) { d.Language = "de" }},
		{"missing outputs", func(d *pipeline.Definition) { d.Outputs = pipeline.Outputs{} }},
		{"missing source", func(d *pipeline.Definition) { d.Source = pipeline.Source{} }},
		{"article translated into its own language", func(d *pipeline.Definition) {
			a, _ := pipeline.NewArticleSpec(1000, []pipeline.Language{pipeline.Turkish})
			d.Outputs, _ = pipeline.NewOutputs(nil, &a, "")
		}},
		{"too many self-review rounds", func(d *pipeline.Definition) { d.Production.SelfReview = pipeline.MaxSelfReview + 1 }},
		{"self-review below off", func(d *pipeline.Definition) { d.Production.SelfReview = pipeline.SelfReviewOff - 1 }},
		{"unknown drafting effort", func(d *pipeline.Definition) { d.Production.Agent.Effort = "turbo" }},
		{"bad drafting model", func(d *pipeline.Definition) { d.Production.Agent.Model = "-x" }},
		{"a template and no template", func(d *pipeline.Definition) { d.TemplateID, d.NoTemplate = uuid.New(), true }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := definition(t, nil)
			tt.mutate(&def)
			_, err := pipeline.New(uuid.New(), uuid.New(), def, t0)
			if !apperror.Is(err, apperror.ValidationFailed) {
				t.Fatalf("err = %v, want ValidationFailed", err)
			}
		})
	}
}

func TestTrigger(t *testing.T) {
	firstSlot := time.Date(2026, 10, 13, 23, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		at       time.Time
		wantErr  bool
		wantNext time.Time
	}{
		{
			name:    "before the slot",
			at:      firstSlot.Add(-time.Second),
			wantErr: true,
		},
		{
			name:     "at the slot",
			at:       firstSlot,
			wantNext: firstSlot.Add(24 * time.Hour),
		},
		{
			name:     "three nights late collapses into one run",
			at:       firstSlot.Add(3*24*time.Hour + time.Hour),
			wantNext: firstSlot.Add(4 * 24 * time.Hour),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPipeline(t, nightly(t, 5))
			run, err := p.Trigger(tt.at)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if !run.Slot.Equal(firstSlot) || run.Count != 5 || run.Trigger != pipeline.TriggerSchedule {
				t.Fatalf("run = %+v", run)
			}
			if !p.NextRunAt().Equal(tt.wantNext) {
				t.Fatalf("next = %v, want %v", p.NextRunAt(), tt.wantNext)
			}
			if p.Due(tt.at) {
				t.Fatal("still due after trigger")
			}
		})
	}
}

func TestPauseResume(t *testing.T) {
	p := newPipeline(t, nightly(t, 1))
	if err := p.Pause(t0); err != nil {
		t.Fatal(err)
	}
	if err := p.Pause(t0); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("second pause err = %v", err)
	}
	if p.Due(t0.Add(48 * time.Hour)) {
		t.Fatal("paused pipeline is due")
	}
	// Resuming two days later skips the missed nights.
	resumeAt := t0.Add(48 * time.Hour)
	if err := p.Resume(resumeAt); err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 15, 23, 0, 0, 0, time.UTC); !p.NextRunAt().Equal(want) {
		t.Fatalf("next = %v, want %v", p.NextRunAt(), want)
	}
	if err := p.Resume(resumeAt); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("second resume err = %v", err)
	}
}

func TestRunNowWorksWhilePausedAndKeepsSchedule(t *testing.T) {
	p := newPipeline(t, nightly(t, 5))
	next := p.NextRunAt()
	run, err := p.RunNow(2, t0)
	if err != nil {
		t.Fatal(err)
	}
	if run.Trigger != pipeline.TriggerManual || run.Count != 2 || run.DedupKey() != "" {
		t.Fatalf("run = %+v", run)
	}
	if !p.NextRunAt().Equal(next) {
		t.Fatal("manual run moved the schedule")
	}
	_ = p.Pause(t0)
	if _, err := p.RunNow(1, t0); err != nil {
		t.Fatalf("manual run while paused: %v", err)
	}
	if _, err := p.RunNow(0, t0); err == nil {
		t.Fatal("zero-count run accepted")
	}
}

func TestRedefineKeepsSlotUnlessScheduleChanges(t *testing.T) {
	p := newPipeline(t, nightly(t, 5))
	slot := p.NextRunAt()

	renamed := definition(t, nightly(t, 5))
	renamed.Name = "Yeni ad"
	if err := p.Redefine(renamed, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !p.NextRunAt().Equal(slot) {
		t.Fatalf("unchanged schedule moved: %v → %v", slot, p.NextRunAt())
	}

	morning, _ := pipeline.ParseSchedule("0 6 * * *", mustLoc(t, "Europe/Istanbul"), 5)
	rescheduled := definition(t, &morning)
	if err := p.Redefine(rescheduled, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC); !p.NextRunAt().Equal(want) {
		t.Fatalf("next = %v, want %v", p.NextRunAt(), want)
	}

	if err := p.Redefine(definition(t, nil), t0); err != nil {
		t.Fatal(err)
	}
	if !p.NextRunAt().IsZero() {
		t.Fatal("dropping the schedule left a next run")
	}
}

func TestUpcoming(t *testing.T) {
	night := func(day int) time.Time { return time.Date(2026, 10, day, 23, 0, 0, 0, time.UTC) }
	week := t0.Add(7 * 24 * time.Hour)
	tests := []struct {
		name  string
		setup func(t *testing.T) *pipeline.Pipeline
		until time.Time
		want  []time.Time
	}{
		{
			name:  "nightly over a week",
			setup: func(t *testing.T) *pipeline.Pipeline { return newPipeline(t, nightly(t, 1)) },
			until: week,
			want:  []time.Time{night(13), night(14), night(15), night(16), night(17), night(18), night(19)},
		},
		{
			name:  "until is exclusive",
			setup: func(t *testing.T) *pipeline.Pipeline { return newPipeline(t, nightly(t, 1)) },
			until: night(14),
			want:  []time.Time{night(13)},
		},
		{
			name: "paused",
			setup: func(t *testing.T) *pipeline.Pipeline {
				p := newPipeline(t, nightly(t, 1))
				if err := p.Pause(t0); err != nil {
					t.Fatal(err)
				}
				return p
			},
			until: week,
		},
		{
			name:  "unscheduled",
			setup: func(t *testing.T) *pipeline.Pipeline { return newPipeline(t, nil) },
			until: week,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.setup(t).Upcoming(tt.until)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("upcoming = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDedupKeyIsStablePerSlot(t *testing.T) {
	a := newPipeline(t, nightly(t, 1))
	slot := a.NextRunAt()
	run, _ := a.Trigger(slot)
	if run.DedupKey() == "" {
		t.Fatal("scheduled run has no dedup key")
	}
	again := pipeline.Run{PipelineID: a.ID(), Trigger: pipeline.TriggerSchedule, Slot: slot}
	if run.DedupKey() != again.DedupKey() {
		t.Fatal("same slot produced different keys")
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	docA := uuid.New()
	pages, _ := pipeline.NewPageRange(3, 9)
	ref, _ := pipeline.NewKnowledgeRef(docA, pages)
	whole, _ := pipeline.NewKnowledgeRef(uuid.New(), pipeline.WholeDocument)
	source, _ := pipeline.NewKnowledgeSource("Sadece kitaptan", []pipeline.KnowledgeRef{ref, whole})
	video, _ := pipeline.NewVideoSpec(pipeline.FormatShort, pipeline.Vertical, 50*time.Second)
	article, _ := pipeline.NewArticleSpec(900, []pipeline.Language{pipeline.English})
	outputs, _ := pipeline.NewOutputs(&video, &article, pipeline.KindArticle)

	def := definition(t, nightly(t, 3))
	def.Source = source
	def.Outputs = outputs
	def.Production = pipeline.Production{Agent: pipeline.Agent{Model: "claude-opus-5-5", Effort: pipeline.EffortMedium}, SelfReview: 3}
	def.NoTemplate = true
	p, err := pipeline.New(uuid.New(), uuid.New(), def, t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Trigger(p.NextRunAt()); err != nil {
		t.Fatal(err)
	}
	got, err := pipeline.Rehydrate(p.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Snapshot(), p.Snapshot()) {
		t.Fatalf("round trip\n got %+v\nwant %+v", got.Snapshot(), p.Snapshot())
	}
}
