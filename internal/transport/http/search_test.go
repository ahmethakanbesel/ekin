package httptransport_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ahmethakanbesel/ekin/internal/adapter/searchconsole"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	"github.com/ahmethakanbesel/ekin/internal/gen"
	analyticssvc "github.com/ahmethakanbesel/ekin/internal/service/analytics/v1"
)

// fakeSearchConsole is one property with one page clicked five times a
// day.
type fakeSearchConsole struct{}

func (fakeSearchConsole) Configured() bool { return true }
func (fakeSearchConsole) AuthURL(state string) string {
	return "https://accounts.example/auth?state=" + url.QueryEscape(state)
}
func (fakeSearchConsole) Exchange(context.Context, string) (string, string, error) {
	return "refresh", "access", nil
}
func (fakeSearchConsole) Access(context.Context, string) (string, error) { return "access", nil }
func (fakeSearchConsole) Properties(context.Context, string) ([]searchconsole.Property, error) {
	return []searchconsole.Property{{URL: "sc-domain:example.com", Permission: "siteOwner"}}, nil
}
func (fakeSearchConsole) Analytics(_ context.Context, _, _ string, q searchconsole.Query) ([]searchconsole.Row, error) {
	var rows []searchconsole.Row
	for d := q.Start; !d.After(q.End); d = d.Add(24 * time.Hour) {
		key := "https://example.com/pricing"
		if q.Dimensions[1] == "query" {
			key = "acme pricing"
		}
		rows = append(rows, searchconsole.Row{Keys: []string{d.Format(time.DateOnly), key}, Clicks: 5, Impressions: 50, Position: 3})
	}
	return rows, nil
}

func TestSearchConsoleAPI(t *testing.T) {
	srv := newServer(t)
	owner := srv.signedIn(t, "owner@example.com")
	ws := owner.workspace("acme")
	base := "/api/v1/workspaces/" + ws.Id.String()

	var sc gen.SearchConsole
	owner.call("GET", base+"/integrations/searchconsole", nil, 200, &sc)
	if !sc.Configured || sc.Connected {
		t.Fatalf("before = %+v", sc)
	}
	var connect struct{ Url string }
	owner.call("POST", base+"/integrations/searchconsole", nil, 200, &connect)
	u, _ := url.Parse(connect.Url)
	state := u.Query().Get("state")

	// Google sends the browser back; Ekin stores the connection and goes
	// back to the settings.
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := noRedirect.Get(srv.URL + "/api/v1/oauth/searchconsole/callback?code=c&state=" + url.QueryEscape(state))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if loc := res.Header.Get("Location"); res.StatusCode != 302 || !strings.HasPrefix(loc, "/w/"+ws.Id.String()+"/settings/publishing?searchconsole=connected") {
		t.Fatalf("callback = %d %s", res.StatusCode, loc)
	}
	owner.call("GET", base+"/integrations/searchconsole", nil, 200, &sc)
	if !sc.Connected || sc.Property == nil || *sc.Property != "sc-domain:example.com" {
		t.Fatalf("after = %+v", sc)
	}

	ctx := context.Background()
	for range 3 {
		j, err := srv.jobs.Claim(ctx, jobdomain.Queues, []string{analyticssvc.KindSync}, "test", time.Minute)
		if err != nil || j == nil {
			break
		}
		var p analyticssvc.SyncPayload
		_ = json.Unmarshal(j.Payload(), &p)
		if err := srv.analytics.RunSync(ctx, p); err != nil {
			t.Fatal(err)
		}
		_ = srv.jobs.Complete(ctx, j.ID(), "test", nil)
	}
	var o gen.SearchOverview
	owner.call("GET", base+"/search?days=7", nil, 200, &o)
	if o.Current == nil || o.Current.Clicks != 35 || o.Previous.Clicks != 35 || len(o.Pages) != 1 || o.Queries[0].Key != "acme pricing" {
		t.Fatalf("overview = %+v", o)
	}
	var props struct {
		Properties []struct{ Url string } `json:"properties"`
	}
	owner.call("GET", base+"/integrations/searchconsole/properties", nil, 200, &props)
	if len(props.Properties) != 1 {
		t.Fatalf("properties = %+v", props)
	}
	owner.fail("PUT", base+"/integrations/searchconsole", map[string]any{"property": "sc-domain:other.org"}, 400, "ValidationFailed")
	owner.call("POST", base+"/integrations/searchconsole/sync", nil, 202, nil)
	owner.call("DELETE", base+"/integrations/searchconsole", nil, 204, nil)
	var empty gen.SearchOverview
	owner.call("GET", base+"/search", nil, 200, &empty)
	if empty.Current != nil || len(empty.Pages) != 0 {
		t.Fatalf("after disconnecting = %+v", empty)
	}
}
