package pipeline_test

import (
	"testing"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	pipeline "ekin/internal/domain/pipeline/v1"
	"ekin/pkg/id"
)

func TestTemplateChoice(t *testing.T) {
	chosen, workspace := id.New(), id.New()
	tests := []struct {
		name             string
		id               uuid.UUID
		none             bool
		workspaceDefault uuid.UUID
		want             uuid.UUID
		wantErr          bool
	}{
		{name: "a chosen template wins over the workspace's", id: chosen, workspaceDefault: workspace, want: chosen},
		{name: "no template beats the workspace's", none: true, workspaceDefault: workspace, want: uuid.Nil},
		{name: "unset inherits the workspace's", workspaceDefault: workspace, want: workspace},
		{name: "unset with no workspace default is none", want: uuid.Nil},
		{name: "a template and no template at once", id: chosen, none: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := pipeline.NewTemplateChoice(tt.id, tt.none)
			if tt.wantErr {
				if !apperror.Is(err, apperror.ValidationFailed) {
					t.Fatalf("err = %v, want ValidationFailed", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := c.Over(tt.workspaceDefault); got != tt.want {
				t.Fatalf("Over = %v, want %v", got, tt.want)
			}
		})
	}
}
