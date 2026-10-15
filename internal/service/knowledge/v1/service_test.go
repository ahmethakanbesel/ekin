package knowledge_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/adapter/extract"
	"kurgu/internal/apperror"
	"kurgu/internal/blob"
	"kurgu/internal/db/dbtest"
	jobdomain "kurgu/internal/domain/job/v1"
	knowledgedomain "kurgu/internal/domain/knowledge/v1"
	workspacedomain "kurgu/internal/domain/workspace/v1"
	jobrepo "kurgu/internal/repository/job/v1"
	knowledgerepo "kurgu/internal/repository/knowledge/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	jobsvc "kurgu/internal/service/job/v1"
	knowledgesvc "kurgu/internal/service/knowledge/v1"
	"kurgu/pkg/id"
)

type fixture struct {
	svc   *knowledgesvc.Service
	jobs  *jobsvc.Service
	blobs blob.Store
	ws    uuid.UUID
}

type usage map[uuid.UUID][]string

func (u usage) PipelinesUsing(_ context.Context, _, documentID uuid.UUID) ([]string, error) {
	return u[documentID], nil
}

func newFixture(t *testing.T, ext knowledgesvc.Extractor, u usage) fixture {
	t.Helper()
	_, tx := dbtest.Open(t)
	w, _ := workspacedomain.New(id.New(), "ws", time.UTC, time.Now())
	if err := workspacerepo.NewSQLite(tx).Create(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	blobs, _ := blob.NewFS(t.TempDir())
	jobs := jobsvc.NewService(jobrepo.NewSQLite(tx), tx)
	svc := knowledgesvc.NewService(knowledgerepo.NewSQLite(tx), blobs, jobs, ext, tx).WithUsage(u)
	return fixture{svc: svc, jobs: jobs, blobs: blobs, ws: w.ID()}
}

// runExtract runs the queued extract job the way the worker would.
func (f fixture) runExtract(t *testing.T) error {
	t.Helper()
	j, err := f.jobs.Claim(context.Background(), []jobdomain.Queue{jobdomain.QueueExtract}, []string{knowledgesvc.KindExtract}, "test", time.Minute)
	if err != nil || j == nil {
		t.Fatalf("claim = %v, %v", j, err)
	}
	var p knowledgesvc.ExtractPayload
	_ = json.Unmarshal(j.Payload(), &p)
	return f.svc.Extract(context.Background(), p.DocumentID)
}

func TestUploadExtractExcerpt(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, extract.New(), usage{})
	text := "Birinci paragraf.\n\n" + strings.Repeat("Kaygı üzerine uzun bir paragraf. ", 120)
	d, err := f.svc.Upload(ctx, f.ws, id.New(), "", "notlar.md", strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	if d.Status() != knowledgedomain.StatusPending || d.Title() != "notlar" || d.Size() != int64(len(text)) || len(d.SHA256()) != 64 {
		t.Fatalf("uploaded = %+v", d.Snapshot())
	}
	if err := f.svc.CheckRange(ctx, f.ws, d.ID(), 0, 0); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("pending doc referencable: %v", err)
	}
	if err := f.runExtract(t); err != nil {
		t.Fatal(err)
	}
	d, _ = f.svc.Get(ctx, f.ws, d.ID())
	if d.Status() != knowledgedomain.StatusReady || d.PageCount() != 2 || d.Tokens() == 0 {
		t.Fatalf("extracted = %+v", d.Snapshot())
	}
	if err := f.svc.CheckRange(ctx, f.ws, d.ID(), 2, 3); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("out-of-range accepted: %v", err)
	}
	ex, err := f.svc.Excerpt(ctx, f.ws, []knowledgesvc.Ref{{DocumentID: d.ID(), From: 2, To: 2}})
	if err != nil || ex.Pages != 1 || !strings.Contains(ex.Markdown, "## notlar, page 2") || strings.Contains(ex.Markdown, "Birinci") {
		t.Fatalf("excerpt = %+v, %v", ex, err)
	}

	_, rc, size, err := f.svc.Open(ctx, f.ws, d.ID())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(b) != text || size != int64(len(text)) {
		t.Fatal("original changed")
	}
}

type failing struct{}

func (failing) Extract(context.Context, knowledgedomain.Format, string) (knowledgedomain.Extraction, error) {
	return knowledgedomain.Extraction{}, errors.New("encrypted PDF")
}

func TestExtractionFailureAndRetry(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, failing{}, usage{})
	d, _ := f.svc.Upload(ctx, f.ws, id.New(), "Book", "book.pdf", strings.NewReader("%PDF-1.4"))
	if err := f.runExtract(t); err != nil {
		t.Fatalf("a bad file must not retry the job: %v", err)
	}
	d, _ = f.svc.Get(ctx, f.ws, d.ID())
	if d.Status() != knowledgedomain.StatusFailed || d.Failure() != "encrypted PDF" {
		t.Fatalf("failed = %+v", d.Snapshot())
	}
	if _, err := f.svc.Retry(ctx, f.ws, d.ID()); err != nil {
		t.Fatal(err)
	}
	_ = f.runExtract(t) // a second job was queued
}

func TestContentsAndReextraction(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, extract.New(), usage{})
	long := strings.Repeat("Uzun bir paragraf. ", 200)
	text := "# Kaygı\n\n" + long + "\n\n## Belirtiler\n\n" + long + "\n\n# Uyku\n\nKısa."
	d, _ := f.svc.Upload(ctx, f.ws, id.New(), "", "notlar.md", strings.NewReader(text))
	if err := f.runExtract(t); err != nil {
		t.Fatal(err)
	}
	d, _ = f.svc.Get(ctx, f.ws, d.ID())
	contents, err := f.svc.Contents(ctx, f.ws, d.ID())
	if err != nil || len(contents) != 3 {
		t.Fatalf("contents = %+v, %v", contents, err)
	}
	last := d.PageCount()
	if c := contents[0]; c.Title != "Kaygı" || c.Level != 1 || c.From != 1 || c.To != contents[2].From-1 {
		t.Fatalf("chapter = %+v of %d pages", c, last)
	}
	if c := contents[2]; c.Title != "Uyku" || c.To != last {
		t.Fatalf("last chapter = %+v of %d pages", c, last)
	}
	if _, err := f.svc.Contents(ctx, id.New(), d.ID()); !apperror.Is(err, apperror.DocumentNotFound) {
		t.Fatalf("other workspace = %v", err)
	}

	// A ready document can be extracted again (to pick up its contents
	// after an extractor change); it keeps its pages meanwhile.
	if _, err := f.svc.Retry(ctx, f.ws, d.ID()); err != nil {
		t.Fatal(err)
	}
	if _, pages, _ := f.svc.Pages(ctx, f.ws, d.ID(), 1, 0); len(pages) != last {
		t.Fatalf("pages while re-extracting = %d", len(pages))
	}
	if err := f.runExtract(t); err != nil {
		t.Fatal(err)
	}
	if again, _ := f.svc.Contents(ctx, f.ws, d.ID()); len(again) != 3 {
		t.Fatalf("contents after re-extraction = %+v", again)
	}
}

func TestDeleteChecksUsage(t *testing.T) {
	ctx := context.Background()
	u := usage{}
	f := newFixture(t, extract.New(), u)
	d, _ := f.svc.Upload(ctx, f.ws, id.New(), "", "a.txt", strings.NewReader("metin"))
	u[d.ID()] = []string{"Shorts"}
	if err := f.svc.Delete(ctx, f.ws, d.ID()); !apperror.Is(err, apperror.Conflict) || !strings.Contains(err.Error(), "Shorts") {
		t.Fatalf("delete in use = %v", err)
	}
	delete(u, d.ID())
	if err := f.svc.Delete(ctx, f.ws, d.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.blobs.Stat(ctx, d.BlobKey()); !errors.Is(err, blob.ErrNotFound) {
		t.Fatalf("blob left behind: %v", err)
	}
	if err := f.runExtract(t); !errors.Is(err, jobdomain.ErrPermanent) {
		t.Fatalf("extract of deleted doc = %v", err)
	}
}

func TestUploadRejectsUnsupported(t *testing.T) {
	f := newFixture(t, extract.New(), usage{})
	if _, err := f.svc.Upload(context.Background(), f.ws, id.New(), "", "a.docx", strings.NewReader("x")); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("docx = %v", err)
	}
	if _, err := f.svc.Upload(context.Background(), f.ws, id.New(), "", "a.txt", strings.NewReader("")); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("empty = %v", err)
	}
}
