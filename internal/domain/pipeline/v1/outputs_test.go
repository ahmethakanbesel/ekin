package pipeline_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	pipeline "ekin/internal/domain/pipeline/v1"
)

func TestNewVideoSpec(t *testing.T) {
	tests := []struct {
		name        string
		format      pipeline.Format
		orientation pipeline.Orientation
		duration    time.Duration
		wantErr     bool
	}{
		{"vertical short", pipeline.FormatShort, pipeline.Vertical, 45 * time.Second, false},
		{"square short", pipeline.FormatShort, pipeline.Square, 60 * time.Second, false},
		{"horizontal short rejected", pipeline.FormatShort, pipeline.Horizontal, 45 * time.Second, true},
		{"short at 3 minutes", pipeline.FormatShort, pipeline.Vertical, 3 * time.Minute, false},
		{"short over 3 minutes", pipeline.FormatShort, pipeline.Vertical, 3*time.Minute + time.Second, true},
		{"short under 10 seconds", pipeline.FormatShort, pipeline.Vertical, 9 * time.Second, true},
		{"long horizontal", pipeline.FormatLong, pipeline.Horizontal, 10 * time.Minute, false},
		{"long under a minute", pipeline.FormatLong, pipeline.Horizontal, 59 * time.Second, true},
		{"long over an hour", pipeline.FormatLong, pipeline.Horizontal, 61 * time.Minute, true},
		{"unknown format", "reel", pipeline.Vertical, time.Minute, true},
		{"unknown orientation", pipeline.FormatShort, "diagonal", time.Minute, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pipeline.NewVideoSpec(tt.format, tt.orientation, tt.duration)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewArticleSpec(t *testing.T) {
	tests := []struct {
		name         string
		words        int
		translations []pipeline.Language
		wantErr      bool
	}{
		{"plain", 1200, nil, false},
		{"with english", 1200, []pipeline.Language{pipeline.English}, false},
		{"too short", 299, nil, true},
		{"too long", 6001, nil, true},
		{"duplicate translation", 1200, []pipeline.Language{pipeline.English, pipeline.English}, true},
		{"unknown translation", 1200, []pipeline.Language{"de"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pipeline.NewArticleSpec(tt.words, tt.translations)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewOutputs(t *testing.T) {
	video, _ := pipeline.NewVideoSpec(pipeline.FormatShort, pipeline.Vertical, 45*time.Second)
	article, _ := pipeline.NewArticleSpec(1200, nil)

	tests := []struct {
		name        string
		video       *pipeline.VideoSpec
		article     *pipeline.ArticleSpec
		lead        pipeline.Kind
		wantErr     bool
		wantLead    pipeline.Kind
		wantDerived pipeline.Kind
	}{
		{name: "nothing", wantErr: true},
		{name: "video only, lead implied", video: &video, wantLead: pipeline.KindVideo},
		{name: "article only, lead implied", article: &article, wantLead: pipeline.KindArticle},
		{name: "video only, lead names the missing article", video: &video, lead: pipeline.KindArticle, wantErr: true},
		{name: "both without lead", video: &video, article: &article, wantErr: true},
		{name: "both, video leads", video: &video, article: &article, lead: pipeline.KindVideo, wantLead: pipeline.KindVideo, wantDerived: pipeline.KindArticle},
		{name: "both, article leads", video: &video, article: &article, lead: pipeline.KindArticle, wantLead: pipeline.KindArticle, wantDerived: pipeline.KindVideo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := pipeline.NewOutputs(tt.video, tt.article, tt.lead)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if o.Lead() != tt.wantLead {
				t.Errorf("lead = %q, want %q", o.Lead(), tt.wantLead)
			}
			derived, ok := o.Derived()
			if ok != (tt.wantDerived != "") || derived != tt.wantDerived {
				t.Errorf("derived = %q,%v, want %q", derived, ok, tt.wantDerived)
			}
			if o.Produces(pipeline.KindVideo) != (tt.video != nil) || o.Produces(pipeline.KindArticle) != (tt.article != nil) {
				t.Errorf("Produces disagrees with the given specs")
			}
		})
	}
}

func TestNewKnowledgeSource(t *testing.T) {
	docA, docB := uuid.New(), uuid.New()
	ref := func(doc uuid.UUID, from, to int) pipeline.KnowledgeRef {
		t.Helper()
		pages, err := pipeline.NewPageRange(from, to)
		if err != nil {
			t.Fatal(err)
		}
		r, err := pipeline.NewKnowledgeRef(doc, pages)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	tests := []struct {
		name    string
		refs    []pipeline.KnowledgeRef
		wantErr bool
	}{
		{"no refs", nil, true},
		{"one whole document", []pipeline.KnowledgeRef{ref(docA, 0, 0)}, false},
		{"two documents", []pipeline.KnowledgeRef{ref(docA, 1, 10), ref(docB, 0, 0)}, false},
		{"disjoint ranges of one document", []pipeline.KnowledgeRef{ref(docA, 1, 10), ref(docA, 11, 20)}, false},
		{"overlapping ranges", []pipeline.KnowledgeRef{ref(docA, 1, 10), ref(docA, 10, 20)}, true},
		{"range inside whole document", []pipeline.KnowledgeRef{ref(docA, 0, 0), ref(docA, 5, 6)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pipeline.NewKnowledgeSource("", tt.refs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewPageRange(t *testing.T) {
	tests := []struct {
		from, to int
		wantErr  bool
	}{
		{0, 0, false},
		{1, 1, false},
		{3, 9, false},
		{0, 5, true},
		{5, 4, true},
		{-1, 2, true},
	}
	for _, tt := range tests {
		_, err := pipeline.NewPageRange(tt.from, tt.to)
		if (err != nil) != tt.wantErr {
			t.Errorf("NewPageRange(%d, %d) err = %v, wantErr %v", tt.from, tt.to, err, tt.wantErr)
		}
	}
}

func TestNewPromptSourceRequiresInstructions(t *testing.T) {
	if _, err := pipeline.NewPromptSource("   "); err == nil {
		t.Fatal("blank instructions accepted")
	}
	if _, err := pipeline.NewPromptSource("Kaygı üzerine kısa videolar"); err != nil {
		t.Fatal(err)
	}
}

func TestVideoSpecWithShorts(t *testing.T) {
	long, _ := pipeline.NewVideoSpec(pipeline.FormatLong, pipeline.Horizontal, 10*time.Minute)
	short, _ := pipeline.NewVideoSpec(pipeline.FormatShort, pipeline.Vertical, 45*time.Second)
	tests := []struct {
		name    string
		spec    pipeline.VideoSpec
		n       int
		wantErr bool
	}{
		{"long with shorts", long, 3, false},
		{"long without", long, 0, false},
		{"long at the ceiling", long, pipeline.MaxShortsPerVideo, false},
		{"over the ceiling", long, pipeline.MaxShortsPerVideo + 1, true},
		{"negative", long, -1, true},
		{"a short cannot be cut", short, 1, true},
		{"a short asks for none", short, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.spec.WithShorts(tt.n)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.Shorts() != tt.n {
				t.Fatalf("shorts = %d", got.Shorts())
			}
			back, err := pipeline.VideoFromSnapshot(pipeline.SnapshotVideo(got))
			if err != nil || back != got {
				t.Fatalf("round trip = %+v, %v", back, err)
			}
		})
	}
	if _, err := pipeline.VideoFromSnapshot(pipeline.VideoSnapshot{Format: "short", Orientation: "vertical", DurationSeconds: 45, Shorts: 2}); err == nil {
		t.Fatal("a stored short asking for shorts rehydrated")
	}
}
