package production_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	pipelinedomain "github.com/ahmethakanbesel/ekin/internal/domain/pipeline/v1"
	visibilitydomain "github.com/ahmethakanbesel/ekin/internal/domain/visibility/v1"
	production "github.com/ahmethakanbesel/ekin/internal/service/production/v1"
	visibilitysvc "github.com/ahmethakanbesel/ekin/internal/service/visibility/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

type fakeVisibility struct {
	gaps    []visibilitysvc.Gap
	covered map[uuid.UUID]uuid.UUID
}

func (f *fakeVisibility) Gaps(_ context.Context, _ uuid.UUID, n int) ([]visibilitysvc.Gap, error) {
	if len(f.gaps) > n {
		return f.gaps[:n], nil
	}
	return f.gaps, nil
}

func (f *fakeVisibility) Cover(_ context.Context, _, promptID, storyID uuid.UUID) error {
	f.covered[promptID] = storyID
	return nil
}

func TestVisibilityPipelineAnswersAGap(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	gap := visibilitysvc.Gap{Prompt: visibilitydomain.Prompt{ID: id.New(), Text: "cheapest tool for small teams"}, Models: 3,
		Competitors: []string{"Globex"}, Citations: []string{"https://review.example/x"}}
	vis := &fakeVisibility{gaps: []visibilitysvc.Gap{gap}, covered: map[uuid.UUID]uuid.UUID{}}
	svc := f.hostWith(func(d *production.Deps) { d.Visibility = vis })
	article, _ := pipelinedomain.NewArticleSpec(900, nil)
	outputs, _ := pipelinedomain.NewOutputs(nil, &article, "")
	source, _ := pipelinedomain.NewVisibilitySource("Guides for small team leads.")
	p, err := f.pipelines.Create(ctx, f.ws, pipelinedomain.Definition{Name: "Gaps", Language: pipelinedomain.English, Outputs: outputs, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pipelines.RunNow(ctx, f.ws, p.ID(), 2); err != nil {
		t.Fatal(err)
	}
	f.drainOn(t, svc)
	st := f.story(t)
	if st.Brief().PromptID != gap.Prompt.ID || st.Brief().Prompt != gap.Prompt.Text || vis.covered[gap.Prompt.ID] != st.ID() {
		t.Fatalf("brief %+v, covered %v", st.Brief(), vis.covered)
	}
	var ideas string
	for _, r := range f.agent.prompts {
		if len(r.JSONSchema) > 0 {
			ideas = r.Prompt
		}
	}
	if !strings.Contains(ideas, `Buyers ask AI assistants: "cheapest tool for small teams"`) || !strings.Contains(ideas, "named Globex instead") {
		t.Fatalf("idea prompt lacks the gap:\n%s", ideas)
	}
	draft := f.agent.prompts[len(f.agent.prompts)-1]
	brief, _ := os.ReadFile(filepath.Join(draft.Dir, "BRIEF.md"))
	if !strings.Contains(string(brief), "Buyers ask AI assistants:") {
		t.Fatalf("brief lacks the prompt:\n%s", brief)
	}
}
