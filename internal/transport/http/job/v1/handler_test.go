package handler_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	handler "github.com/ahmethakanbesel/ekin/internal/transport/http/job/v1"
)

func TestToWireReadsTheSubject(t *testing.T) {
	story, deliverable, trace := uuid.New(), uuid.New(), uuid.New()
	payload, _ := json.Marshal(map[string]any{"storyId": story, "deliverableId": deliverable, "final": true, "other": 1})
	j, err := jobdomain.New(uuid.New(), jobdomain.Spec{
		WorkspaceID: uuid.New(), Queue: jobdomain.QueueRender, Kind: "deliverable.render", Payload: payload, TraceID: trace,
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	w := handler.ToWire(j)
	if w.TraceId != trace || w.StartedAt != nil {
		t.Fatalf("trace %s started %v", w.TraceId, w.StartedAt)
	}
	s := w.Subject
	if s.StoryId == nil || *s.StoryId != story || s.DeliverableId == nil || *s.DeliverableId != deliverable ||
		s.Final == nil || !*s.Final || s.PipelineId != nil || s.DocumentId != nil {
		t.Fatalf("subject = %+v", s)
	}
	_ = j.Start("w", time.Minute, time.Now())
	if handler.ToWire(j).StartedAt == nil {
		t.Fatal("no start after Start")
	}
}
