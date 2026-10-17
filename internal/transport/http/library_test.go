package httptransport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	jobdomain "kurgu/internal/domain/job/v1"
	librarydomain "kurgu/internal/domain/library/v1"
	"kurgu/internal/gen"
	librarysvc "kurgu/internal/service/library/v1"
)

// fakeProcessor "ingests" a photo by copying it as main.jpg with a
// poster, and anything else as a 10 s portrait clip.
type fakeProcessor struct{}

func (fakeProcessor) Ingest(_ context.Context, kind librarydomain.Kind, in, outDir string) (librarydomain.Media, error) {
	raw, err := os.ReadFile(in) //nolint:gosec // test temp file
	if err != nil {
		return librarydomain.Media{}, err
	}
	format := map[librarydomain.Kind]string{librarydomain.KindPhoto: "jpg", librarydomain.KindVideo: "webm", librarydomain.KindMusic: "opus"}[kind]
	_ = os.WriteFile(filepath.Join(outDir, "main."+format), raw, 0o600)
	_ = os.WriteFile(filepath.Join(outDir, "poster.jpg"), []byte("poster"), 0o600)
	return librarydomain.Media{Format: format, Width: 1080, Height: 1920, Duration: 10 * time.Second, Poster: kind != librarydomain.KindMusic}, nil
}

func (fakeProcessor) Inspect(context.Context, librarydomain.Kind, string, string) (librarydomain.Media, error) {
	return librarydomain.Media{}, errors.New("not used here")
}

func (fakeProcessor) Preview(context.Context, string, string) error { return errors.New("not used here") }

// uploadAsset posts a library upload and decodes the reply into out.
func (c *client) uploadAsset(path, filename, kind string, details any, content []byte, wantStatus int, out any) {
	c.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if kind != "" {
		_ = w.WriteField("kind", kind)
	}
	if details != nil {
		raw, _ := json.Marshal(details)
		_ = w.WriteField("details", string(raw))
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
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != wantStatus {
		c.t.Fatalf("upload %s = %d %s, want %d", filename, res.StatusCode, raw, wantStatus)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		c.t.Fatalf("decode %s: %v", raw, err)
	}
}

func (s *server) ingest(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	j, err := s.jobs.Claim(ctx, []jobdomain.Queue{jobdomain.QueueExtract}, []string{librarysvc.KindIngest}, "t", time.Minute)
	if err != nil || j == nil {
		t.Fatalf("claim = %v, %v", j, err)
	}
	var p librarysvc.IngestPayload
	_ = json.Unmarshal(j.Payload(), &p)
	if err := s.library.Ingest(ctx, p.AssetID); err != nil {
		t.Fatal(err)
	}
}

func TestLibrary(t *testing.T) {
	srv := newServer(t)
	owner := srv.signedIn(t, "owner@example.com")
	ws := owner.workspace("acme")
	base := "/api/v1/workspaces/" + ws.Id.String() + "/library/assets"

	details := map[string]any{"title": "Dağ manzarası", "tags": []string{"Dağ", "sabah"}, "credit": map[string]any{"author": "Ayşe", "url": "", "licence": ""}}
	var photo gen.LibraryAsset
	owner.uploadAsset(base, "dag.jpg", "photo", details, []byte("jpeg bytes"), 202, &photo)
	if photo.Status != gen.LibraryAssetStatusProcessing || photo.Kind != gen.LibraryAssetKindPhoto || photo.Title != "Dağ manzarası" ||
		strings.Join(photo.Tags, ",") != "dağ,sabah" || photo.Credit.Author != "Ayşe" || photo.Origin != gen.LibraryAssetOriginUpload {
		t.Fatalf("upload = %+v", photo)
	}
	assetPath := base + "/" + photo.Id.String()
	owner.fail("GET", assetPath+"/files/main", nil, 404, "AssetNotFound")
	srv.ingest(t)
	owner.call("GET", assetPath, nil, 200, &photo)
	if photo.Status != gen.LibraryAssetStatusReady || photo.Orientation == nil || *photo.Orientation != gen.OrientationPortrait || !photo.HasPoster {
		t.Fatalf("ingested = %+v", photo)
	}

	if photo.UsageCount != 0 {
		t.Fatalf("usage count = %d", photo.UsageCount)
	}
	var usages struct{ Usages []gen.LibraryUsage }
	owner.call("GET", assetPath+"/usages", nil, 200, &usages)
	if usages.Usages == nil || len(usages.Usages) != 0 {
		t.Fatalf("usages = %+v", usages)
	}

	// The main file streams with Range support.
	res := owner.do("GET", assetPath+"/files/main", nil, http.Header{"Range": {"bytes=0-3"}})
	raw, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusPartialContent || string(raw) != "jpeg" || res.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("range = %d %q %s", res.StatusCode, raw, res.Header.Get("Content-Type"))
	}
	owner.fail("GET", assetPath+"/files/original", nil, 404, "AssetNotFound")

	// Music takes its kind from the file name; the same file twice is a
	// conflict; a file of the wrong kind is refused.
	var music gen.LibraryAsset
	owner.uploadAsset(base, "track.mp3", "", nil, []byte("mp3 bytes"), 202, &music)
	if music.Kind != gen.LibraryAssetKindMusic || music.Title != "track" {
		t.Fatalf("music = %+v", music)
	}
	var e gen.Error
	owner.uploadAsset(base, "again.mp3", "", nil, []byte("mp3 bytes"), 409, &e)
	if owner.uploadAsset(base, "a.mp3", "video", nil, []byte("x"), 400, &e); e.Code != "ValidationFailed" {
		t.Fatalf("wrong kind = %+v", e)
	}
	if owner.uploadAsset(base, "a.gif", "", nil, []byte("x"), 400, &e); e.Code != "ValidationFailed" {
		t.Fatalf("gif = %+v", e)
	}

	var page struct {
		Assets     []gen.LibraryAsset
		NextCursor *string
	}
	owner.call("GET", base+"?kind=photo&q=DAG", nil, 200, &page)
	if len(page.Assets) != 1 || page.Assets[0].Id != photo.Id {
		t.Fatalf("filtered = %+v", page.Assets)
	}
	owner.call("GET", base+"?limit=1", nil, 200, &page)
	if len(page.Assets) != 1 || page.NextCursor == nil {
		t.Fatalf("first page = %+v", page)
	}
	owner.fail("GET", base+"?kind=gif", nil, 400, "ValidationFailed")

	// Edit details, archive, and the archived asset leaves the default listing.
	owner.call("PATCH", assetPath, map[string]any{
		"details":  map[string]any{"title": "Zirve", "tags": []string{"dağ"}, "description": "Karlı zirve"},
		"archived": true,
	}, 200, &photo)
	if photo.Title != "Zirve" || photo.ArchivedAt == nil || photo.Credit.Author != "" {
		t.Fatalf("patched = %+v", photo)
	}
	owner.call("GET", base+"?kind=photo", nil, 200, &page)
	if len(page.Assets) != 0 {
		t.Fatalf("archived listed = %+v", page.Assets)
	}
	owner.call("GET", base+"?kind=photo&archived=true", nil, 200, &page)
	if len(page.Assets) != 1 {
		t.Fatalf("archived missing = %+v", page.Assets)
	}
	owner.fail("PATCH", assetPath, map[string]any{"details": map[string]any{"title": "x", "mood": "calm"}}, 400, "ValidationFailed")
	owner.fail("POST", assetPath+"/retry", nil, 409, "InvalidTransition")

	// Outsiders see nothing; a viewer may look but not change.
	stranger := srv.signedIn(t, "stranger@example.com")
	stranger.fail("GET", base, nil, 404, "WorkspaceNotFound")
	stranger.fail("GET", assetPath+"/files/main", nil, 404, "WorkspaceNotFound")
	var created gen.CreateInvitation201JSONResponse
	owner.call("POST", "/api/v1/workspaces/"+ws.Id.String()+"/invitations", map[string]any{"email": "viewer@example.com", "role": "viewer"}, 201, &created)
	viewer := srv.client(t)
	viewer.call("POST", "/api/v1/invitations/"+strings.TrimPrefix(created.Url, publicURL+"/invite/")+"/accept",
		map[string]any{"name": "Vera", "password": "a-good-password"}, 200, nil)
	viewer.call("GET", assetPath, nil, 200, nil)
	viewer.fail("DELETE", assetPath, nil, 403, "Forbidden")
	if viewer.uploadAsset(base, "b.jpg", "", nil, []byte("y"), 403, &e); e.Code != "Forbidden" {
		t.Fatalf("viewer upload = %+v", e)
	}

	owner.call("DELETE", assetPath, nil, 204, nil)
	owner.fail("GET", assetPath, nil, 404, "AssetNotFound")
	srv.client(t).fail("GET", assetPath+"/files/main", nil, 401, "Unauthenticated")
}

// A pipeline's production settings carry each kind's media source; an
// absent kind reads back as fresh.
func TestPipelineMediaSources(t *testing.T) {
	srv := newServer(t)
	owner := srv.signedIn(t, "owner@example.com")
	ws := owner.workspace("acme")
	base := "/api/v1/workspaces/" + ws.Id.String() + "/pipelines"

	def := shortsDefinition()
	def["production"] = map[string]any{"media": map[string]any{"music": "library", "video": "mixed"}}
	var p gen.Pipeline
	owner.call("POST", base, def, 201, &p)
	m := p.Definition.Production.Media
	if m == nil || *m.Music != gen.MediaSourceLibrary || *m.Video != gen.MediaSourceMixed || *m.Photo != gen.MediaSourceFresh {
		t.Fatalf("media = %+v", m)
	}

	def["production"] = map[string]any{"media": map[string]any{"music": "fresh"}}
	var redefined gen.Pipeline
	owner.call("PUT", base+"/"+p.Id.String(), map[string]any{"version": p.Version, "definition": def}, 200, &redefined)
	if redefined.Definition.Production != nil {
		t.Fatalf("all fresh should read as defaults: %+v", redefined.Definition.Production)
	}

	def["production"] = map[string]any{"media": map[string]any{"photo": "everything"}}
	owner.fail("POST", base, def, 400, "ValidationFailed")
}
