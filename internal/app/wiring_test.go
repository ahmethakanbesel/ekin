package app

import (
	"context"
	"testing"
)

// A cancelled request must not make a working tool look broken: the
// check result is cached for later requests.
func TestProbeToolIgnoresCancelledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ok, detail := probeTool(ctx, "go", "version"); !ok {
		t.Fatalf("probe with a cancelled request = %v %q", ok, detail)
	}
	if ok, _ := probeTool(context.Background(), "ekin-no-such-tool"); ok {
		t.Fatal("missing tool reported ok")
	}
}
