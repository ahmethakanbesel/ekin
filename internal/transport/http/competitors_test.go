package httptransport_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	"github.com/ahmethakanbesel/ekin/internal/gen"
	sitesvc "github.com/ahmethakanbesel/ekin/internal/service/site/v1"
)

func (s *server) runCompetitorChecks(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for range 5 {
		j, err := s.jobs.Claim(ctx, jobdomain.Queues, []string{sitesvc.KindCompetitorCheck}, "test", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if j == nil {
			return
		}
		var p sitesvc.CompetitorPayload
		_ = json.Unmarshal(j.Payload(), &p)
		if err := s.site.RunCompetitorCheck(ctx, p); err != nil {
			t.Fatal(err)
		}
		_ = s.jobs.Complete(ctx, j.ID(), "test", nil)
	}
}

func TestCompetitorsAPI(t *testing.T) {
	var week atomic.Int32
	var rival *httptest.Server
	rival = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			extra := ""
			if week.Load() > 0 {
				extra = `<url><loc>` + rival.URL + `/vs/acme</loc></url>`
			}
			_, _ = w.Write([]byte(`<urlset><url><loc>` + rival.URL + `/</loc></url>` + extra + `</urlset>`))
		case "/vs/acme":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><title>Globex vs Acme</title></head></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer rival.Close()

	srv := newServer(t)
	owner := srv.signedIn(t, "owner@example.com")
	ws := owner.workspace("acme")
	base := "/api/v1/workspaces/" + ws.Id.String() + "/site/competitors"

	var c gen.Competitor
	owner.call("POST", base, map[string]any{"url": rival.URL, "name": "Globex"}, 201, &c)
	owner.fail("POST", base, map[string]any{"url": rival.URL + "/"}, 409, "Conflict")
	srv.runCompetitorChecks(t)
	var list gen.ListCompetitors200JSONResponse
	owner.call("GET", base, nil, 200, &list)
	if len(list.Competitors) != 1 || list.Competitors[0].Pages != 1 || list.Competitors[0].LastCheckAt == nil {
		t.Fatalf("after the baseline = %+v", list)
	}

	week.Store(1)
	owner.call("POST", base+"/"+c.Id.String()+"/check", nil, 202, nil)
	srv.runCompetitorChecks(t)
	var changes gen.ListCompetitorChanges200JSONResponse
	owner.call("GET", base+"/changes?kind=added", nil, 200, &changes)
	if len(changes.Changes) != 1 || changes.Changes[0].Title != "Globex vs Acme" || changes.Changes[0].Url != rival.URL+"/vs/acme" {
		t.Fatalf("changes = %+v", changes)
	}
	owner.call("DELETE", base+"/"+c.Id.String(), nil, 204, nil)
	var none gen.ListCompetitorChanges200JSONResponse
	owner.call("GET", base+"/changes", nil, 200, &none)
	if len(none.Changes) != 0 {
		t.Fatalf("changes after removing = %+v", none)
	}
}
