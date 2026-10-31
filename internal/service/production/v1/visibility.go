package production

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	visibilitysvc "github.com/ahmethakanbesel/ekin/internal/service/visibility/v1"
)

// Visibility hands out buyer prompts AI assistants answered without the
// brand (the visibility service).
type Visibility interface {
	Gaps(ctx context.Context, workspaceID uuid.UUID, n int) ([]visibilitysvc.Gap, error)
	// Cover records that a story answers the prompt.
	Cover(ctx context.Context, workspaceID, promptID, storyID uuid.UUID) error
}

// describeGap is the source window an idea for a visibility gap draws on.
func describeGap(g visibilitysvc.Gap) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Buyers ask AI assistants: %q. In the latest check, %d assistant(s) answered without naming the brand", g.Prompt.Text, g.Models)
	if len(g.Competitors) > 0 {
		fmt.Fprintf(&b, "; they named %s instead", strings.Join(g.Competitors, ", "))
	}
	b.WriteString(".")
	if len(g.Citations) > 0 {
		fmt.Fprintf(&b, " They cited: %s.", strings.Join(g.Citations, ", "))
	}
	b.WriteString(" The idea must answer this question directly and well enough to be the source an assistant cites.")
	return b.String()
}
