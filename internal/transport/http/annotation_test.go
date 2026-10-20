package httptransport_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ahmethakanbesel/ekin/internal/gen"
)

func TestAnnotationAPI(t *testing.T) {
	srv := newServer(t)
	owner := srv.signedIn(t, "owner@example.com")
	ws := owner.workspace("acme")
	storyID, videoID := srv.narratedVideo(t, ws.Id, true)
	deliverable := fmt.Sprintf("/api/v1/workspaces/%s/stories/%s/deliverables/%s", ws.Id, storyID, videoID)
	note := deliverable + "/revisions/1/annotation"

	var me gen.User
	owner.call("GET", "/api/v1/me", nil, 200, &me)
	var rev gen.Revision
	owner.call("PUT", note, map[string]any{"text": "  Müziği Ayşe seçti; bir sonraki videoda da kullan.  "}, 200, &rev)
	if rev.Number != 1 || rev.Annotation == nil || rev.Annotation.Text != "Müziği Ayşe seçti; bir sonraki videoda da kullan." ||
		rev.Annotation.By != me.Id || rev.Annotation.At.IsZero() {
		t.Fatalf("annotated revision = %+v", rev)
	}
	owner.call("PUT", note, map[string]any{"text": "Ses onaylandı."}, 200, &rev)
	var revs struct {
		Revisions []gen.Revision `json:"revisions"`
	}
	owner.call("GET", deliverable+"/revisions", nil, 200, &revs)
	if a := revs.Revisions[0].Annotation; a == nil || a.Text != "Ses onaylandı." {
		t.Fatalf("listed annotation = %+v", a)
	}

	owner.fail("PUT", note, map[string]any{"text": strings.Repeat("a", 4001)}, 400, "ValidationFailed")
	owner.fail("PUT", deliverable+"/revisions/2/annotation", map[string]any{"text": "yok"}, 404, "NotFound")

	var cleared gen.Revision
	owner.call("PUT", note, map[string]any{"text": " "}, 200, &cleared)
	if cleared.Annotation != nil {
		t.Fatalf("cleared annotation = %+v", cleared.Annotation)
	}

	// A viewer reads notes but cannot write them; outsiders see nothing.
	var created gen.CreateInvitation201JSONResponse
	owner.call("POST", "/api/v1/workspaces/"+ws.Id.String()+"/invitations", map[string]any{"email": "viewer@example.com", "role": "viewer"}, 201, &created)
	viewer := srv.client(t)
	viewer.call("POST", "/api/v1/invitations/"+strings.TrimPrefix(created.Url, publicURL+"/invite/")+"/accept",
		map[string]any{"name": "Vera", "password": "a-good-password"}, 200, nil)
	viewer.call("GET", deliverable+"/revisions", nil, 200, nil)
	viewer.fail("PUT", note, map[string]any{"text": "benden"}, 403, "Forbidden")
	stranger := srv.signedIn(t, "stranger@example.com")
	stranger.workspace("other")
	stranger.fail("PUT", note, map[string]any{"text": "benden"}, 404, "WorkspaceNotFound")
}
