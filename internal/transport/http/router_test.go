package httptransport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
	"unicode"

	"kurgu/internal/adapter/extract"
	"kurgu/internal/blob"
	"kurgu/internal/db/dbtest"
	identitydomain "kurgu/internal/domain/identity/v1"
	jobdomain "kurgu/internal/domain/job/v1"
	settingsdomain "kurgu/internal/domain/settings/v1"
	"kurgu/internal/gen"
	identityrepo "kurgu/internal/repository/identity/v1"
	jobrepo "kurgu/internal/repository/job/v1"
	knowledgerepo "kurgu/internal/repository/knowledge/v1"
	pipelinerepo "kurgu/internal/repository/pipeline/v1"
	settingsrepo "kurgu/internal/repository/settings/v1"
	templaterepo "kurgu/internal/repository/template/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	identitysvc "kurgu/internal/service/identity/v1"
	jobsvc "kurgu/internal/service/job/v1"
	knowledgesvc "kurgu/internal/service/knowledge/v1"
	pipelinesvc "kurgu/internal/service/pipeline/v1"
	settingssvc "kurgu/internal/service/settings/v1"
	templatesvc "kurgu/internal/service/template/v1"
	workspacesvc "kurgu/internal/service/workspace/v1"
	httptransport "kurgu/internal/transport/http"
	systemhandler "kurgu/internal/transport/http/system/v1"
	"kurgu/pkg/password"
	"kurgu/pkg/secretbox"
)

const publicURL = "https://kurgu.example.com"

type server struct {
	*httptest.Server
	identity  *identitysvc.Service
	knowledge *knowledgesvc.Service
	jobs      *jobsvc.Service
	logs      *syncBuffer
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newServer(t *testing.T) *server {
	t.Helper()
	_, tx := dbtest.Open(t)
	identities := identityrepo.NewSQLite(tx)
	workspaces := workspacerepo.NewSQLite(tx)
	jobs := jobsvc.NewService(jobrepo.NewSQLite(tx), tx)
	identity := identitysvc.NewService(identities, workspaces, tx, password.New(password.Params{Memory: 64, Time: 1, Threads: 1}))
	logs := &syncBuffer{}
	blobs, _ := blob.NewFS(t.TempDir())
	knowledge := knowledgesvc.NewService(knowledgerepo.NewSQLite(tx), blobs, jobs, extract.New(), tx)
	pipelines := pipelinesvc.NewService(pipelinerepo.NewSQLite(tx), workspaces, jobs, tx).WithDocuments(knowledge)
	knowledge.WithUsage(pipelines)
	box, _ := secretbox.New([]byte("router-test-secret-0123"))
	settings := settingssvc.NewService(settingsrepo.NewSQLite(tx), tx, settingsdomain.Profile{Name: "default"}, slog.New(slog.DiscardHandler)).
		WithCredentials(settingsdomain.Credentials{DecisionModel: "typesafe/jev-1.13"}, box)
	templates := templatesvc.NewService(templaterepo.NewSQLite(tx), jobs, nil, t.TempDir())
	pipelines.WithTemplates(templates)
	router, err := httptransport.NewRouter(httptransport.Deps{
		Templates:  templates,
		System:     systemhandler.New(settings, "default", "test", nil),
		Identity:   identity,
		Workspaces: workspacesvc.NewService(workspaces, identities, tx),
		Pipelines:  pipelines,
		Jobs:       jobs,
		Knowledge:  knowledge,
		PublicURL:  publicURL,
		UI:         fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Kurgu</title>")}},
		Log:        slog.New(slog.NewJSONHandler(logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return &server{Server: srv, identity: identity, knowledge: knowledge, jobs: jobs, logs: logs}
}

// client is a browser: it keeps cookies.
type client struct {
	t   *testing.T
	srv *server
	h   *http.Client
}

func (s *server) client(t *testing.T) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, srv: s, h: &http.Client{Jar: jar}}
}

// signedIn creates an account and returns a client signed in to it.
func (s *server) signedIn(t *testing.T, email string) *client {
	t.Helper()
	e, _ := identitydomain.ParseEmail(email)
	p, _ := identitydomain.ParsePassword("password-" + email)
	if _, err := s.identity.CreateUser(context.Background(), e, "User "+email, p); err != nil {
		t.Fatal(err)
	}
	c := s.client(t)
	c.call("POST", "/api/v1/auth/login", map[string]any{"email": email, "password": "password-" + email}, 200, nil)
	return c
}

func (c *client) do(method, path string, body any, header http.Header) *http.Response {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.srv.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header[k] = v
	}
	res, err := c.h.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	return res
}

func (c *client) call(method, path string, body any, wantStatus int, out any) {
	c.t.Helper()
	res := c.do(method, path, body, nil)
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != wantStatus {
		c.t.Fatalf("%s %s = %d %s, want %d", method, path, res.StatusCode, raw, wantStatus)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			c.t.Fatalf("decode %s: %v", raw, err)
		}
	}
}

// fail calls and checks the error code.
func (c *client) fail(method, path string, body any, wantStatus int, wantCode string) {
	c.t.Helper()
	var e gen.Error
	c.call(method, path, body, wantStatus, &e)
	if e.Code != wantCode {
		c.t.Fatalf("%s %s code = %s (%s), want %s", method, path, e.Code, e.Message, wantCode)
	}
}

func (c *client) workspace(name string) gen.Workspace {
	c.t.Helper()
	var ws gen.Workspace
	c.call("POST", "/api/v1/workspaces", map[string]any{"name": name, "timezone": "Europe/Istanbul"}, 201, &ws)
	return ws
}

func shortsDefinition() map[string]any {
	return map[string]any{
		"name":     "Psikoloji shorts",
		"language": "tr",
		"outputs": map[string]any{
			"video": map[string]any{"format": "short", "orientation": "vertical", "durationSeconds": 45},
		},
		"source":   map[string]any{"kind": "prompt", "instructions": "Kaygı ve stres üzerine eğitici kısa videolar."},
		"schedule": map[string]any{"cron": "0 2 * * *", "timezone": "Europe/Istanbul", "batch": 5},
	}
}

func TestPipelineLifecycle(t *testing.T) {
	c := newServer(t).signedIn(t, "owner@example.com")
	ws := c.workspace("acme")
	if ws.Role != gen.RoleOwner {
		t.Fatalf("creator role = %s", ws.Role)
	}
	base := "/api/v1/workspaces/" + ws.Id.String() + "/pipelines"

	var p gen.Pipeline
	c.call("POST", base, shortsDefinition(), 201, &p)
	if p.Status != "active" || p.NextRunAt == nil || p.Definition.Outputs.Lead == nil || *p.Definition.Outputs.Lead != "video" {
		t.Fatalf("created pipeline = %+v", p)
	}
	if p.Definition.Schedule == nil || p.Definition.Schedule.Timezone != "Europe/Istanbul" {
		t.Fatalf("schedule = %+v", p.Definition.Schedule)
	}

	var list struct{ Pipelines []gen.Pipeline }
	c.call("GET", base, nil, 200, &list)
	if len(list.Pipelines) != 1 {
		t.Fatalf("list = %d", len(list.Pipelines))
	}

	var upcoming gen.ListUpcomingRuns200JSONResponse
	c.call("GET", "/api/v1/workspaces/"+ws.Id.String()+"/upcoming-runs?days=2", nil, 200, &upcoming)
	if len(upcoming.Runs) != 2 || upcoming.Runs[0].PipelineId != p.Id || !upcoming.Runs[0].At.Equal(*p.NextRunAt) || upcoming.Runs[0].Count != 5 {
		t.Fatalf("upcoming = %+v", upcoming)
	}
	c.call("GET", "/api/v1/workspaces/"+ws.Id.String()+"/upcoming-runs?days=15", nil, 400, nil)

	def := shortsDefinition()
	def["name"] = "Renamed"
	var updated gen.Pipeline
	c.call("PUT", base+"/"+p.Id.String(), map[string]any{"version": p.Version, "definition": def}, 200, &updated)
	if updated.Definition.Name != "Renamed" || updated.Version != p.Version+1 {
		t.Fatalf("updated = %+v", updated)
	}
	var stale gen.Error
	c.call("PUT", base+"/"+p.Id.String(), map[string]any{"version": p.Version, "definition": def}, 409, &stale)
	if stale.Code != "StaleVersion" {
		t.Fatalf("stale code = %s", stale.Code)
	}

	var paused gen.Pipeline
	c.call("POST", base+"/"+p.Id.String()+"/pause", nil, 200, &paused)
	if paused.Status != "paused" || paused.NextRunAt != nil {
		t.Fatalf("paused = %+v", paused)
	}
	var again gen.Error
	c.call("POST", base+"/"+p.Id.String()+"/pause", nil, 409, &again)

	var job gen.Job
	c.call("POST", base+"/"+p.Id.String()+"/runs", map[string]any{"count": 2}, 202, &job)
	if job.Kind != pipelinesvc.KindRun || job.Status != "queued" {
		t.Fatalf("job = %+v", job)
	}
	var jobs struct{ Jobs []gen.Job }
	c.call("GET", "/api/v1/workspaces/"+ws.Id.String()+"/jobs?status=queued", nil, 200, &jobs)
	if len(jobs.Jobs) != 1 || jobs.Jobs[0].Id != job.Id {
		t.Fatalf("jobs = %+v", jobs)
	}
	var cancelled gen.Job
	c.call("POST", "/api/v1/workspaces/"+ws.Id.String()+"/jobs/"+job.Id.String()+"/cancel", nil, 200, &cancelled)
	if cancelled.Status != "cancelled" {
		t.Fatalf("cancelled = %+v", cancelled)
	}

	c.call("DELETE", base+"/"+p.Id.String(), nil, 204, nil)
	var missing gen.Error
	c.call("GET", base+"/"+p.Id.String(), nil, 404, &missing)
	if missing.Code != "PipelineNotFound" {
		t.Fatalf("missing code = %s", missing.Code)
	}
}

func TestValidationErrors(t *testing.T) {
	c := newServer(t).signedIn(t, "owner@example.com")
	ws := c.workspace("acme")
	base := "/api/v1/workspaces/" + ws.Id.String() + "/pipelines"

	tests := []struct {
		name   string
		mutate func(d map[string]any)
	}{
		{"no outputs", func(d map[string]any) { d["outputs"] = map[string]any{} }},
		{"horizontal short", func(d map[string]any) {
			d["outputs"] = map[string]any{"video": map[string]any{"format": "short", "orientation": "horizontal", "durationSeconds": 45}}
		}},
		{"both outputs without lead", func(d map[string]any) {
			d["outputs"] = map[string]any{
				"video":   map[string]any{"format": "short", "orientation": "vertical", "durationSeconds": 45},
				"article": map[string]any{"words": 900, "translations": []string{"en"}},
			}
		}},
		{"every-minute cron", func(d map[string]any) {
			d["schedule"] = map[string]any{"cron": "* * * * *", "timezone": "Europe/Istanbul", "batch": 1}
		}},
		{"host-local timezone", func(d map[string]any) {
			d["schedule"] = map[string]any{"cron": "0 2 * * *", "timezone": "Local", "batch": 1}
		}},
		{"prompt source with refs", func(d map[string]any) {
			d["source"] = map[string]any{"kind": "prompt", "instructions": "x", "refs": []map[string]any{{"documentId": ws.Id.String()}}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := shortsDefinition()
			tt.mutate(d)
			var e gen.Error
			c.call("POST", base, d, 400, &e)
			if e.Code != "ValidationFailed" || e.Message == "" {
				t.Fatalf("error = %+v", e)
			}
		})
	}

	var bad gen.Error
	c.call("POST", base, "not an object", 400, &bad)
	if bad.Code != "BadRequest" {
		t.Fatalf("malformed body code = %s", bad.Code)
	}
	c.fail("POST", "/api/v1/workspaces/00000000-0000-7000-8000-000000000000/pipelines", shortsDefinition(), 404, "WorkspaceNotFound")
}

func TestSPAFallbackAndUnknownAPI(t *testing.T) {
	srv := newServer(t)
	c := srv.client(t)
	res, err := http.Get(srv.URL + "/pipelines/some-client-route")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 || !bytes.Contains(body, []byte("<title>Kurgu</title>")) {
		t.Fatalf("spa fallback = %d %s", res.StatusCode, body)
	}
	var e gen.Error
	c.call("GET", "/api/v1/nope", nil, 404, &e)
	if e.Code != "NotFound" {
		t.Fatalf("unknown api code = %s", e.Code)
	}
}

func TestSessionLifecycle(t *testing.T) {
	srv := newServer(t)
	anon := srv.client(t)
	anon.fail("GET", "/api/v1/me", nil, 401, "Unauthenticated")
	anon.fail("GET", "/api/v1/workspaces", nil, 401, "Unauthenticated")
	anon.fail("POST", "/api/v1/auth/logout", nil, 401, "Unauthenticated")

	c := srv.signedIn(t, "ada@example.com")
	anon.fail("POST", "/api/v1/auth/login", map[string]any{"email": "ada@example.com", "password": "nope"}, 401, "Unauthenticated")

	res := c.do("POST", "/api/v1/auth/login", map[string]any{"email": "ADA@example.com", "password": "password-ada@example.com"}, nil)
	_ = res.Body.Close()
	cookie := res.Header.Get("Set-Cookie")
	for _, want := range []string{"kurgu_session=", "HttpOnly", "Secure", "SameSite=Lax", "Path=/"} {
		if !strings.Contains(cookie, want) {
			t.Fatalf("cookie %q lacks %s", cookie, want)
		}
	}
	if res.Header.Get("Cache-Control") != "no-store" || res.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("security headers = %v", res.Header)
	}

	var me gen.User
	c.call("GET", "/api/v1/me", nil, 200, &me)
	if me.Email != "ada@example.com" {
		t.Fatalf("me = %+v", me)
	}
	c.call("PATCH", "/api/v1/me", map[string]any{"name": "Ada Lovelace"}, 200, &me)
	if me.Name != "Ada Lovelace" {
		t.Fatalf("renamed = %+v", me)
	}
	c.fail("POST", "/api/v1/me/password", map[string]any{"currentPassword": "wrong", "newPassword": "a-new-password"}, 400, "ValidationFailed")
	c.call("POST", "/api/v1/me/password", map[string]any{"currentPassword": "password-ada@example.com", "newPassword": "a-new-password"}, 204, nil)
	c.call("GET", "/api/v1/me", nil, 200, nil)

	res = c.do("POST", "/api/v1/auth/logout", nil, nil)
	_ = res.Body.Close()
	if res.StatusCode != 204 || !strings.Contains(res.Header.Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("logout = %d %q", res.StatusCode, res.Header.Get("Set-Cookie"))
	}
	c.fail("GET", "/api/v1/me", nil, 401, "Unauthenticated")
}

func TestRolesAndInvitations(t *testing.T) {
	srv := newServer(t)
	owner := srv.signedIn(t, "owner@example.com")
	ws := owner.workspace("acme")
	base := "/api/v1/workspaces/" + ws.Id.String()

	invite := func(email, role string) string {
		t.Helper()
		var created gen.CreateInvitation201JSONResponse
		owner.call("POST", base+"/invitations", map[string]any{"email": email, "role": role}, 201, &created)
		if !strings.HasPrefix(created.Url, publicURL+"/invite/") || created.Invitation.Email != email {
			t.Fatalf("invitation = %+v", created)
		}
		return strings.TrimPrefix(created.Url, publicURL+"/invite/")
	}
	join := func(token, name string) *client {
		t.Helper()
		c := srv.client(t)
		var preview gen.InvitationPreview
		c.call("GET", "/api/v1/invitations/"+token, nil, 200, &preview)
		if preview.WorkspaceName != "acme" || preview.AccountExists {
			t.Fatalf("preview = %+v", preview)
		}
		var accepted struct {
			WorkspaceID string `json:"workspaceId"`
			User        gen.User
		}
		c.call("POST", "/api/v1/invitations/"+token+"/accept", map[string]any{"name": name, "password": "a-good-password"}, 200, &accepted)
		if accepted.WorkspaceID != ws.Id.String() || accepted.User.Name != name {
			t.Fatalf("accepted = %+v", accepted)
		}
		return c
	}

	viewerToken := invite("viewer@example.com", "viewer")
	var pending struct{ Invitations []gen.Invitation }
	owner.call("GET", base+"/invitations", nil, 200, &pending)
	if len(pending.Invitations) != 1 {
		t.Fatalf("pending = %+v", pending)
	}
	viewer := join(viewerToken, "Vera")
	editor := join(invite("editor@example.com", "editor"), "Ed")
	srv.client(t).fail("GET", "/api/v1/invitations/"+viewerToken, nil, 404, "InvitationNotFound")

	var mine struct{ Workspaces []gen.Workspace }
	viewer.call("GET", "/api/v1/workspaces", nil, 200, &mine)
	if len(mine.Workspaces) != 1 || mine.Workspaces[0].Role != gen.RoleViewer {
		t.Fatalf("viewer workspaces = %+v", mine)
	}

	// viewer: read only
	viewer.call("GET", base+"/pipelines", nil, 200, nil)
	viewer.call("GET", base+"/jobs", nil, 200, nil)
	viewer.call("GET", base+"/members", nil, 200, nil)
	viewer.fail("POST", base+"/pipelines", shortsDefinition(), 403, "Forbidden")
	viewer.fail("PATCH", base, map[string]any{"name": "mine now"}, 403, "Forbidden")
	viewer.fail("GET", base+"/invitations", nil, 403, "Forbidden")

	// editor: content, not settings or members
	var p gen.Pipeline
	editor.call("POST", base+"/pipelines", shortsDefinition(), 201, &p)
	editor.call("POST", base+"/pipelines/"+p.Id.String()+"/runs", map[string]any{"count": 1}, 202, nil)
	editor.fail("PATCH", base, map[string]any{"name": "x"}, 403, "Forbidden")
	editor.fail("POST", base+"/invitations", map[string]any{"email": "x@example.com", "role": "owner"}, 403, "Forbidden")

	// strangers cannot tell the workspace exists
	stranger := srv.signedIn(t, "stranger@example.com")
	stranger.fail("GET", base, nil, 404, "WorkspaceNotFound")
	stranger.fail("GET", base+"/pipelines/"+p.Id.String(), nil, 404, "WorkspaceNotFound")

	// members
	var members struct{ Members []gen.Member }
	owner.call("GET", base+"/members", nil, 200, &members)
	if len(members.Members) != 3 || members.Members[0].Role != gen.RoleOwner {
		t.Fatalf("members = %+v", members)
	}
	var vera gen.Member
	for _, m := range members.Members {
		if m.Name == "Vera" {
			vera = m
		}
	}
	var ownerID string
	for _, m := range members.Members {
		if m.Role == gen.RoleOwner {
			ownerID = m.UserId.String()
		}
	}
	owner.fail("PATCH", base+"/members/"+ownerID, map[string]any{"role": "editor"}, 409, "LastOwner")
	owner.fail("DELETE", base+"/members/"+ownerID, nil, 409, "LastOwner")
	editor.fail("DELETE", base+"/members/"+vera.UserId.String(), nil, 403, "Forbidden")
	var changed gen.Member
	owner.call("PATCH", base+"/members/"+vera.UserId.String(), map[string]any{"role": "editor"}, 200, &changed)
	if changed.Role != gen.RoleEditor {
		t.Fatalf("changed = %+v", changed)
	}
	viewer.call("POST", base+"/pipelines/"+p.Id.String()+"/pause", nil, 200, nil)
	viewer.call("DELETE", base+"/members/"+vera.UserId.String(), nil, 204, nil) // leave
	viewer.fail("GET", base, nil, 404, "WorkspaceNotFound")

	// The token is never logged: the route pattern is.
	if strings.Contains(srv.logs.String(), viewerToken) || !strings.Contains(srv.logs.String(), "/invitations/{token}") {
		t.Fatalf("token handling in logs:\n%s", srv.logs.String())
	}
}

func TestCrossOriginWritesRefused(t *testing.T) {
	srv := newServer(t)
	c := srv.signedIn(t, "ada@example.com")
	tests := []struct {
		name   string
		header http.Header
		want   int
	}{
		{"cross-site fetch", http.Header{"Sec-Fetch-Site": {"cross-site"}}, 403},
		{"foreign origin", http.Header{"Origin": {"https://evil.example"}}, 403},
		{"same origin", http.Header{"Sec-Fetch-Site": {"same-origin"}}, 201},
		{"public url origin", http.Header{"Origin": {publicURL}}, 201},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := c.do("POST", "/api/v1/workspaces", map[string]any{"name": "x", "timezone": "UTC"}, tt.header)
			_ = res.Body.Close()
			if res.StatusCode != tt.want {
				t.Fatalf("status = %d, want %d", res.StatusCode, tt.want)
			}
		})
	}
}

// Every operation in the spec is either public by design or guarded, and
// every workspace operation has an access rule.
func TestAccessRulesCoverSpec(t *testing.T) {
	spec, err := gen.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	var public, unruled []string
	seen := map[string]bool{}
	for path, item := range spec.Paths.Map() {
		for _, op := range item.Operations() {
			name := string(unicode.ToUpper(rune(op.OperationID[0]))) + op.OperationID[1:]
			if op.Security != nil && len(*op.Security) == 0 {
				public = append(public, name)
				continue
			}
			if strings.Contains(path, "{workspaceId}") {
				seen[name] = true
				if _, ok := httptransport.WorkspaceActions[name]; !ok {
					unruled = append(unruled, name)
				}
			}
		}
	}
	slices.Sort(public)
	if want := []string{"AcceptInvitation", "GetInvitation", "Login", "YouTubeCallback"}; !slices.Equal(public, want) {
		t.Fatalf("public operations = %v, want %v", public, want)
	}
	if len(unruled) > 0 {
		t.Fatalf("workspace operations without an access rule: %v", unruled)
	}
	for name := range httptransport.WorkspaceActions {
		if !seen[name] {
			t.Errorf("access rule for unknown operation %s", name)
		}
	}
}

func (c *client) upload(path, filename, title string, content []byte) *http.Response {
	c.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if title != "" {
		_ = w.WriteField("title", title)
	}
	fw, _ := w.CreateFormFile("file", filename)
	_, _ = fw.Write(content)
	_ = w.Close()
	req, _ := http.NewRequest("POST", c.srv.URL+path, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	res, err := c.h.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	return res
}

func TestKnowledgeBase(t *testing.T) {
	srv := newServer(t)
	owner := srv.signedIn(t, "owner@example.com")
	ws := owner.workspace("acme")
	base := "/api/v1/workspaces/" + ws.Id.String()

	res := owner.upload(base+"/documents", "notes.md", "Kaygı notları", []byte("# Kaygı\n\nİlk sayfa."))
	var doc gen.Document
	_ = json.NewDecoder(res.Body).Decode(&doc)
	_ = res.Body.Close()
	if res.StatusCode != 202 || doc.Status != gen.DocumentStatusPending || doc.Title != "Kaygı notları" || doc.Format != gen.DocumentFormatMd {
		t.Fatalf("upload = %d %+v", res.StatusCode, doc)
	}
	docPath := base + "/documents/" + doc.Id.String()

	// A pipeline cannot reference it before extraction.
	def := shortsDefinition()
	def["source"] = map[string]any{"kind": "knowledge", "instructions": "", "refs": []map[string]any{{"documentId": doc.Id.String()}}}
	owner.fail("POST", base+"/pipelines", def, 400, "ValidationFailed")

	j, err := srv.jobs.Claim(context.Background(), []jobdomain.Queue{jobdomain.QueueExtract}, []string{knowledgesvc.KindExtract}, "t", time.Minute)
	if err != nil || j == nil {
		t.Fatalf("claim = %v", err)
	}
	if err := srv.knowledge.Extract(context.Background(), doc.Id); err != nil {
		t.Fatal(err)
	}
	owner.call("GET", docPath, nil, 200, &doc)
	if doc.Status != gen.DocumentStatusReady || doc.PageCount != 1 || doc.Tokens == 0 {
		t.Fatalf("extracted = %+v", doc)
	}
	var pages struct{ Pages []gen.DocumentPage }
	owner.call("GET", docPath+"/pages?from=1&to=1", nil, 200, &pages)
	if len(pages.Pages) != 1 || pages.Pages[0].Text != "# Kaygı\n\nİlk sayfa." {
		t.Fatalf("pages = %+v", pages)
	}
	res = owner.do("GET", docPath+"/file", nil, nil)
	raw, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 || string(raw) != "# Kaygı\n\nİlk sayfa." {
		t.Fatalf("file = %d %q", res.StatusCode, raw)
	}

	def["source"] = map[string]any{"kind": "knowledge", "instructions": "", "refs": []map[string]any{{"documentId": doc.Id.String(), "fromPage": 1, "toPage": 2}}}
	owner.fail("POST", base+"/pipelines", def, 400, "ValidationFailed")
	def["source"] = map[string]any{"kind": "knowledge", "instructions": "", "refs": []map[string]any{{"documentId": doc.Id.String(), "fromPage": 1, "toPage": 1}}}
	var p gen.Pipeline
	owner.call("POST", base+"/pipelines", def, 201, &p)
	owner.fail("DELETE", docPath, nil, 409, "Conflict")
	owner.call("DELETE", base+"/pipelines/"+p.Id.String(), nil, 204, nil)
	owner.call("DELETE", docPath, nil, 204, nil)

	res = owner.upload(base+"/documents", "slides.pptx", "", []byte("x"))
	_ = res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("pptx upload = %d", res.StatusCode)
	}
}

// Listings page by cursor and filter through the query string; a cursor
// the server did not make is a 400.
func TestListingsPageAndFilter(t *testing.T) {
	srv := newServer(t)
	owner := srv.signedIn(t, "owner@example.com")
	ws := owner.workspace("acme")
	base := "/api/v1/workspaces/" + ws.Id.String()
	for _, title := range []string{"Kaygı notları", "Uyku", "KAYGI ve beden"} {
		res := owner.upload(base+"/documents", "notes.md", title, []byte("# Not"))
		_ = res.Body.Close()
		if res.StatusCode != 202 {
			t.Fatalf("upload = %d", res.StatusCode)
		}
	}

	var titles []string
	path := base + "/documents?limit=2"
	for range 5 {
		var page struct {
			Documents  []gen.Document
			NextCursor *string
		}
		owner.call("GET", path, nil, 200, &page)
		for _, d := range page.Documents {
			titles = append(titles, d.Title)
		}
		if page.NextCursor == nil {
			break
		}
		path = base + "/documents?limit=2&cursor=" + url.QueryEscape(*page.NextCursor)
	}
	if !slices.Equal(titles, []string{"KAYGI ve beden", "Uyku", "Kaygı notları"}) {
		t.Fatalf("paged titles = %q", titles)
	}

	var found struct{ Documents []gen.Document }
	owner.call("GET", base+"/documents?q=kaygi&status=pending&format=md", nil, 200, &found)
	if len(found.Documents) != 2 {
		t.Fatalf("search = %d documents", len(found.Documents))
	}
	owner.call("GET", base+"/documents?status=ready", nil, 200, &found)
	if len(found.Documents) != 0 {
		t.Fatalf("ready = %d documents", len(found.Documents))
	}

	// Each upload queued its own extraction: three requests, paged.
	var jobs struct {
		Jobs       []gen.Job
		NextCursor *string
	}
	owner.call("GET", base+"/jobs?limit=2&kind="+knowledgesvc.KindExtract, nil, 200, &jobs)
	if len(jobs.Jobs) != 2 || jobs.NextCursor == nil {
		t.Fatalf("jobs page 1 = %d, next %v", len(jobs.Jobs), jobs.NextCursor)
	}
	next := *jobs.NextCursor
	jobs.Jobs, jobs.NextCursor = nil, nil
	owner.call("GET", base+"/jobs?limit=2&kind="+knowledgesvc.KindExtract+"&cursor="+url.QueryEscape(next), nil, 200, &jobs)
	if len(jobs.Jobs) != 1 || jobs.NextCursor != nil {
		t.Fatalf("jobs page 2 = %d, next %v", len(jobs.Jobs), jobs.NextCursor)
	}

	for _, list := range []string{"/documents", "/jobs"} {
		owner.fail("GET", base+list+"?cursor=not-ours", nil, 400, "ValidationFailed")
	}
	owner.fail("GET", base+"/documents?status=lost", nil, 400, "ValidationFailed")
	owner.fail("GET", base+"/documents?format=pptx", nil, 400, "ValidationFailed")
}

// Feedback positions keep their precision (they were float32 once and
// came back as 14.199999809265137).
func TestFeedbackPositionIsDouble(t *testing.T) {
	var body struct {
		Notes []struct {
			AtSeconds *float64 `json:"atSeconds"`
		} `json:"notes"`
	}
	raw, _ := json.Marshal(gen.FeedbackInput{Notes: []gen.FeedbackNoteInput{{Text: "x", AtSeconds: ptr(14.2)}}})
	_ = json.Unmarshal(raw, &body)
	if len(body.Notes) != 1 || body.Notes[0].AtSeconds == nil || *body.Notes[0].AtSeconds != 14.2 {
		t.Fatalf("notes = %s", raw)
	}
}

func ptr[T any](v T) *T { return &v }

func TestCredentialsAreAdminOnlyAndHidden(t *testing.T) {
	srv := newServer(t)
	admin := srv.signedIn(t, "admin@example.com") // first account: admin
	other := srv.signedIn(t, "other@example.com")

	var out struct{ Credentials []gen.Credential }
	admin.call("PATCH", "/api/v1/system/credentials", map[string]any{"values": map[string]string{"pexelsApiKey": "secret-pexels-9876", "claudeModel": "sonnet"}}, 200, &out)
	got := map[string]gen.Credential{}
	for _, c := range out.Credentials {
		got[c.Key] = c
	}
	if c := got["pexelsApiKey"]; c.Source != "settings" || c.Value == nil || *c.Value != "••••9876" {
		t.Fatalf("pexels = %+v", c)
	}
	if c := got["claudeModel"]; c.Value == nil || *c.Value != "sonnet" {
		t.Fatalf("model = %+v", c)
	}
	if c := got["decisionModel"]; c.Source != "env" {
		t.Fatalf("decision model = %+v", c)
	}
	other.fail("GET", "/api/v1/system/credentials", nil, 403, "Forbidden")
	other.fail("PATCH", "/api/v1/system/credentials", map[string]any{"values": map[string]string{"pexelsApiKey": "x"}}, 403, "Forbidden")
	admin.fail("PATCH", "/api/v1/system/credentials", map[string]any{"values": map[string]string{"smtpPort": "abc"}}, 400, "ValidationFailed")
}

func TestTemplatesAPI(t *testing.T) {
	srv := newServer(t)
	owner := srv.signedIn(t, "owner@example.com")
	ws := owner.workspace("acme")
	base := "/api/v1/workspaces/" + ws.Id.String()

	var list struct{ Templates []gen.Template }
	owner.call("GET", base+"/templates", nil, 200, &list)
	var cosmic gen.Template
	for _, tm := range list.Templates {
		if tm.Name == "Cosmic flat explainer" {
			cosmic = tm
		}
	}
	if cosmic.Source != gen.TemplateSourceBuiltin || len(cosmic.Palette) == 0 {
		t.Fatalf("built-in = %+v", cosmic)
	}
	owner.fail("PUT", base+"/templates/"+cosmic.Id.String(), map[string]any{"name": "x", "kinds": []string{"video"}, "guide": "g"}, 403, "Forbidden")
	var copyT gen.Template
	owner.call("POST", base+"/templates/"+cosmic.Id.String()+"/duplicate", nil, 201, &copyT)
	owner.call("PUT", base+"/templates/"+copyT.Id.String(), map[string]any{"name": "Ours", "kinds": []string{"video"}, "guide": "## Look\n- calm", "palette": []string{"#112233"}, "headingFont": "Oswald"}, 200, &copyT)
	if copyT.Name != "Ours" || copyT.HeadingFont != "Oswald" || copyT.Source != gen.TemplateSourceCustom {
		t.Fatalf("updated = %+v", copyT)
	}

	def := shortsDefinition()
	def["templateId"] = copyT.Id.String()
	var p gen.Pipeline
	owner.call("POST", base+"/pipelines", def, 201, &p)
	if p.Definition.TemplateId == nil || *p.Definition.TemplateId != copyT.Id {
		t.Fatalf("pipeline template = %v", p.Definition.TemplateId)
	}
	def["templateId"] = "00000000-0000-7000-8000-000000000001"
	owner.fail("POST", base+"/pipelines", def, 400, "ValidationFailed")
	owner.call("DELETE", base+"/templates/"+copyT.Id.String(), nil, 204, nil)
}
