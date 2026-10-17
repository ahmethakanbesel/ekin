package library_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db/dbtest"
	librarydomain "kurgu/internal/domain/library/v1"
	workspacedomain "kurgu/internal/domain/workspace/v1"
	libraryrepo "kurgu/internal/repository/library/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	"kurgu/pkg/id"
)

var now = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

func impls() map[string]func(t *testing.T) (libraryrepo.Repository, uuid.UUID) {
	return map[string]func(t *testing.T) (libraryrepo.Repository, uuid.UUID){
		"inmemory": func(*testing.T) (libraryrepo.Repository, uuid.UUID) { return libraryrepo.NewInMemory(), id.New() },
		"sqlite": func(t *testing.T) (libraryrepo.Repository, uuid.UUID) {
			_, tx := dbtest.Open(t)
			w, _ := workspacedomain.New(id.New(), "ws", time.UTC, now)
			if err := workspacerepo.NewSQLite(tx).Create(context.Background(), w); err != nil {
				t.Fatal(err)
			}
			return libraryrepo.NewSQLite(tx), w.ID()
		},
	}
}

func newAsset(t *testing.T, ws uuid.UUID, filename, sha string, d librarydomain.Details, at time.Time) *librarydomain.Asset {
	t.Helper()
	a, err := librarydomain.NewUpload(id.New(), ws, id.New(), "", librarydomain.Upload{Filename: filename, Size: 10, SHA256: sha}, d, at)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestConformance(t *testing.T) {
	for name, open := range impls() {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repo, ws := open(t)
			music := newAsset(t, ws, "Sabah.mp3", "m", librarydomain.Details{Tags: []string{"piano"}, Mood: "calm", Tempo: 72,
				Credit: librarydomain.Credit{Author: "Ayşe", URL: "https://example.com"}}, now)
			clip := newAsset(t, ws, "deniz.mp4", "v", librarydomain.Details{Description: "Dalgalı deniz"}, now.Add(time.Minute))
			photo := newAsset(t, ws, "dağ.jpg", "p", librarydomain.Details{}, now.Add(2*time.Minute))
			for _, a := range []*librarydomain.Asset{music, clip, photo} {
				if err := repo.Create(ctx, a); err != nil {
					t.Fatal(err)
				}
			}
			dup := newAsset(t, ws, "again.mp3", "m", librarydomain.Details{}, now)
			if err := repo.Create(ctx, dup); !apperror.Is(err, apperror.Conflict) {
				t.Fatalf("duplicate hash err = %v", err)
			}
			_ = music.Ingested(librarydomain.Media{Format: "opus", Duration: 95500 * time.Millisecond}, now)
			_ = clip.Ingested(librarydomain.Media{Format: "webm", Width: 1080, Height: 1920, Duration: 12 * time.Second, Poster: true}, now)
			_ = photo.Ingested(librarydomain.Media{Format: "jpg", Width: 3000, Height: 2000}, now)
			photo.Archive(now.Add(time.Hour))
			for _, a := range []*librarydomain.Asset{music, clip, photo} {
				if err := repo.Update(ctx, a); err != nil {
					t.Fatal(err)
				}
			}
			got, err := repo.Get(ctx, ws, music.ID())
			if err != nil || !reflect.DeepEqual(got.Snapshot(), music.Snapshot()) {
				t.Fatalf("get = %+v, %v\nwant %+v", got.Snapshot(), err, music.Snapshot())
			}
			if got, err := repo.GetByID(ctx, photo.ID()); err != nil || !reflect.DeepEqual(got.Snapshot(), photo.Snapshot()) {
				t.Fatalf("get by id = %+v, %v", got, err)
			}
			if _, err := repo.Get(ctx, id.New(), music.ID()); !apperror.Is(err, apperror.AssetNotFound) {
				t.Fatalf("other workspace err = %v", err)
			}
			if got, err := repo.FindBySHA256(ctx, ws, "v"); err != nil || got.ID() != clip.ID() {
				t.Fatalf("find by hash = %v, %v", got, err)
			}
			if _, err := repo.FindBySHA256(ctx, id.New(), "v"); !apperror.Is(err, apperror.AssetNotFound) {
				t.Fatalf("hash in other workspace err = %v", err)
			}

			ids := func(f libraryrepo.Filter) []uuid.UUID {
				t.Helper()
				list, _, err := repo.List(ctx, ws, f)
				if err != nil {
					t.Fatal(err)
				}
				out := []uuid.UUID{}
				for _, a := range list {
					out = append(out, a.ID())
				}
				return out
			}
			tests := []struct {
				name string
				f    libraryrepo.Filter
				want []uuid.UUID
			}{
				{"unarchived, newest first", libraryrepo.Filter{}, []uuid.UUID{clip.ID(), music.ID()}},
				{"with archived", libraryrepo.Filter{Archived: true}, []uuid.UUID{photo.ID(), clip.ID(), music.ID()}},
				{"kind", libraryrepo.Filter{Kinds: []librarydomain.Kind{librarydomain.KindMusic}}, []uuid.UUID{music.ID()}},
				{"orientation", libraryrepo.Filter{Archived: true, Orientations: []librarydomain.Orientation{librarydomain.OrientationLandscape}}, []uuid.UUID{photo.ID()}},
				{"origin", libraryrepo.Filter{Origins: []librarydomain.Origin{librarydomain.OriginPexels}}, []uuid.UUID{}},
				{"status", libraryrepo.Filter{Statuses: []librarydomain.Status{librarydomain.StatusReady}}, []uuid.UUID{clip.ID(), music.ID()}},
				{"query in tags", libraryrepo.Filter{Query: "PIANO"}, []uuid.UUID{music.ID()}},
				{"query in description, folded", libraryrepo.Filter{Query: "dalgali"}, []uuid.UUID{clip.ID()}},
				{"query in title, folded", libraryrepo.Filter{Archived: true, Query: "dag"}, []uuid.UUID{photo.ID()}},
			}
			for _, tt := range tests {
				if got := ids(tt.f); !reflect.DeepEqual(got, tt.want) {
					t.Errorf("%s = %v, want %v", tt.name, got, tt.want)
				}
			}

			first, next, err := repo.List(ctx, ws, libraryrepo.Filter{Archived: true, Limit: 2})
			if err != nil || len(first) != 2 || next == "" {
				t.Fatalf("first page = %d, %q, %v", len(first), next, err)
			}
			rest, last, err := repo.List(ctx, ws, libraryrepo.Filter{Archived: true, Limit: 2, After: next})
			if err != nil || len(rest) != 1 || rest[0].ID() != music.ID() || last != "" {
				t.Fatalf("second page = %v, %q, %v", rest, last, err)
			}
			if _, _, err := repo.List(ctx, ws, libraryrepo.Filter{After: "nope"}); !apperror.Is(err, apperror.ValidationFailed) {
				t.Fatalf("bad cursor err = %v", err)
			}

			if err := repo.Delete(ctx, ws, clip.ID()); err != nil {
				t.Fatal(err)
			}
			if err := repo.Delete(ctx, ws, clip.ID()); !apperror.Is(err, apperror.AssetNotFound) {
				t.Fatalf("delete twice err = %v", err)
			}
			if err := repo.Update(ctx, clip); !apperror.Is(err, apperror.AssetNotFound) {
				t.Fatalf("update deleted err = %v", err)
			}
		})
	}
}
