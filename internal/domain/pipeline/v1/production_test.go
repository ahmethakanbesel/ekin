package pipeline_test

import (
	"testing"

	pipeline "kurgu/internal/domain/pipeline/v1"
)

func TestNewAgent(t *testing.T) {
	tests := []struct {
		name, model, effort string
		want                pipeline.Agent
		wantErr             bool
	}{
		{name: "defaults", want: pipeline.Agent{}},
		{name: "model id and effort", model: "claude-sonnet-5-5", effort: "low", want: pipeline.Agent{Model: "claude-sonnet-5-5", Effort: pipeline.EffortLow}},
		{name: "alias with context suffix", model: " opus[1m] ", want: pipeline.Agent{Model: "opus[1m]"}},
		{name: "effort only", effort: "xhigh", want: pipeline.Agent{Effort: pipeline.EffortXHigh}},
		{name: "unknown effort", effort: "turbo", wantErr: true},
		{name: "flag injection", model: "--dangerously-skip-permissions", wantErr: true},
		{name: "spaces inside", model: "claude sonnet", wantErr: true},
		{name: "too long", model: "claude-" + string(make([]byte, 70)), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pipeline.NewAgent(tt.model, tt.effort)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
