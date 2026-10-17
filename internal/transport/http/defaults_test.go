package httptransport_test

import (
	"strings"
	"testing"

	"kurgu/internal/gen"
)

// A workspace's production defaults: any member reads them with the
// server's values beside them; only a manager changes them.
func TestWorkspaceProduction(t *testing.T) {
	srv := newServer(t)
	owner := srv.signedIn(t, "owner@example.com")
	ws := owner.workspace("acme")
	base := "/api/v1/workspaces/" + ws.Id.String()
	path := base + "/production"

	var got gen.WorkspaceProduction
	owner.call("GET", path, nil, 200, &got)
	wantServer := gen.ServerProduction{DraftModelSource: gen.ServerProductionDraftModelSourceUnset,
		PreviewHeight: 540, FinalHeight: 1080, PreviewFps: 30, FinalFps: 30, Profile: "default"}
	if got.Server.DraftEffort == nil || *got.Server.DraftEffort != gen.DraftEffortHigh {
		t.Fatalf("server effort = %v", got.Server.DraftEffort)
	}
	got.Server.DraftEffort = nil
	if got.Server != wantServer || got.SelfReviewRounds != nil || got.Defaults != (gen.Production{}) {
		t.Fatalf("unset = %+v", got)
	}

	saved := map[string]any{
		"defaults": map[string]any{
			"draftModel": "opus", "draftEffort": "max", "previewQuality": "720p", "finalFps": 60,
			"reviewAt": "script", "media": map[string]any{"music": "library", "photo": "fresh"}, "thumbnails": 0,
		},
		"selfReviewRounds": 0,
	}
	owner.call("PUT", path, saved, 200, &got)
	var again gen.WorkspaceProduction
	owner.call("GET", path, nil, 200, &again)
	d := again.Defaults
	switch {
	case d.DraftModel == nil || *d.DraftModel != "opus", d.DraftEffort == nil || *d.DraftEffort != gen.DraftEffortMax,
		d.PreviewQuality == nil || *d.PreviewQuality != gen.ProductionPreviewQualityN720p, d.FinalQuality != nil,
		d.FinalFps == nil || *d.FinalFps != gen.ProductionFinalFpsN60, d.PreviewFps != nil,
		d.ReviewAt == nil || *d.ReviewAt != gen.ProductionReviewAtScript,
		d.Media == nil || d.Media.Music == nil || *d.Media.Music != gen.MediaSourceLibrary || d.Media.Video != nil ||
			d.Media.Photo == nil || *d.Media.Photo != gen.MediaSourceFresh,
		d.Thumbnails == nil || *d.Thumbnails != 0:
		t.Fatalf("saved defaults = %+v", d)
	}
	if again.SelfReviewRounds == nil || *again.SelfReviewRounds != 0 {
		t.Fatalf("self-review rounds = %v, want 0 (off)", again.SelfReviewRounds)
	}

	// Saving replaces: what is left out goes back to the server's.
	var replaced gen.WorkspaceProduction
	owner.call("PUT", path, map[string]any{"defaults": map[string]any{"reviewAt": "video"}}, 200, &replaced)
	if replaced.SelfReviewRounds != nil || replaced.Defaults.DraftModel != nil || replaced.Defaults.ReviewAt == nil ||
		*replaced.Defaults.ReviewAt != gen.ProductionReviewAtVideo {
		t.Fatalf("replaced = %+v", replaced)
	}

	for _, body := range map[string]map[string]any{
		"too many rounds":     {"defaults": map[string]any{}, "selfReviewRounds": 6},
		"negative rounds":     {"defaults": map[string]any{}, "selfReviewRounds": -1},
		"bad final quality":   {"defaults": map[string]any{"finalQuality": "360p"}},
		"bad preview fps":     {"defaults": map[string]any{"previewFps": 60}},
		"unknown media":       {"defaults": map[string]any{"media": map[string]any{"video": "everything"}}},
		"bad model":           {"defaults": map[string]any{"draftModel": "-x"}},
		"too many thumbnails": {"defaults": map[string]any{"thumbnails": 5}},
	} {
		owner.fail("PUT", path, body, 400, "ValidationFailed")
	}

	invite := func(email, role string) *client {
		var created gen.CreateInvitation201JSONResponse
		owner.call("POST", base+"/invitations", map[string]any{"email": email, "role": role}, 201, &created)
		c := srv.client(t)
		c.call("POST", "/api/v1/invitations/"+strings.TrimPrefix(created.Url, publicURL+"/invite/")+"/accept",
			map[string]any{"name": role, "password": "a-good-password"}, 200, nil)
		return c
	}
	viewer := invite("viewer@example.com", "viewer")
	var seen gen.WorkspaceProduction
	viewer.call("GET", path, nil, 200, &seen)
	if seen.Defaults.ReviewAt == nil || seen.Defaults.DraftModel != nil {
		t.Fatalf("viewer sees %+v", seen)
	}
	viewer.fail("PUT", path, map[string]any{"defaults": map[string]any{}}, 403, "Forbidden")
	editor := invite("editor@example.com", "editor")
	editor.call("GET", path, nil, 200, nil)
	editor.fail("PUT", path, map[string]any{"defaults": map[string]any{}}, 403, "Forbidden")
	srv.signedIn(t, "stranger@example.com").fail("GET", path, nil, 404, "WorkspaceNotFound")
}

// A pipeline's self-review rounds read back only when set: omitted is
// the workspace default, 0 is off.
func TestPipelineSelfReviewRounds(t *testing.T) {
	srv := newServer(t)
	owner := srv.signedIn(t, "owner@example.com")
	ws := owner.workspace("acme")
	base := "/api/v1/workspaces/" + ws.Id.String() + "/pipelines"
	n := func(v int) *int { return &v }
	tests := []struct {
		name string
		in   *int
		want *int
	}{
		{"omitted", nil, nil},
		{"off", n(0), n(0)},
		{"one", n(1), n(1)},
		{"five", n(5), n(5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := shortsDefinition()
			if tt.in != nil {
				def["selfReviewRounds"] = *tt.in
			}
			var p gen.Pipeline
			owner.call("POST", base, def, 201, &p)
			got := p.Definition.SelfReviewRounds
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Fatalf("selfReviewRounds = %v, want %v", got, tt.want)
			}
		})
	}
	def := shortsDefinition()
	def["selfReviewRounds"] = 6
	owner.fail("POST", base, def, 400, "ValidationFailed")
}
