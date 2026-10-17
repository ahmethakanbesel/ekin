package library_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	"ekin/internal/db/dbtest"
	librarydomain "ekin/internal/domain/library/v1"
	workspacedomain "ekin/internal/domain/workspace/v1"
	libraryrepo "ekin/internal/repository/library/v1"
	workspacerepo "ekin/internal/repository/workspace/v1"
	"ekin/pkg/id"
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

			usage := func(asset uuid.UUID, deliverable uuid.UUID, revision int, at time.Time) {
				t.Helper()
				u, err := librarydomain.NewUsage(asset, ws, id.New(), deliverable, revision, at)
				if err != nil {
					t.Fatal(err)
				}
				if err := repo.RecordUsage(ctx, u); err != nil {
					t.Fatal(err)
				}
			}
			d1, d2 := id.New(), id.New()
			usage(clip.ID(), d1, 1, now)
			usage(clip.ID(), d1, 2, now.Add(time.Hour))
			usage(clip.ID(), d1, 2, now.Add(2*time.Hour)) // the same revision again
			usage(clip.ID(), d2, 1, now.Add(3*time.Hour))
			usage(music.ID(), d2, 1, now.Add(3*time.Hour))
			if err := repo.RecordUsage(ctx, librarydomain.Usage{AssetID: clip.ID(), WorkspaceID: id.New(), StoryID: id.New(),
				DeliverableID: id.New(), Revision: 1, At: now}); err != nil {
				t.Fatalf("usage in another workspace err = %v", err)
			}
			counts, err := repo.UsageCounts(ctx, ws, []uuid.UUID{clip.ID(), music.ID(), photo.ID()})
			if err != nil || !reflect.DeepEqual(counts, map[uuid.UUID]int{clip.ID(): 2, music.ID(): 1}) {
				t.Fatalf("counts = %v, %v", counts, err)
			}
			uses, err := repo.Usages(ctx, ws, clip.ID())
			if err != nil || len(uses) != 3 || uses[0].DeliverableID != d2 || uses[2].Revision != 1 || !uses[1].At.Equal(now.Add(time.Hour)) {
				t.Fatalf("usages = %+v, %v", uses, err)
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
			if uses, _ := repo.Usages(ctx, ws, clip.ID()); len(uses) != 0 {
				t.Fatalf("usages outlived their asset: %d", len(uses))
			}
		})
	}
}

func TestHarvestConformance(t *testing.T) {
	for name, open := range impls() {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repo, ws := open(t)
			deliverable := id.New()
			harvested := func(ref, sha string, at time.Time) *librarydomain.Asset {
				t.Helper()
				h := librarydomain.Harvest{StoryID: id.New(), DeliverableID: deliverable, Revision: 1, SourceRef: ref,
					SourceURL: "https://www.pexels.com/video/1/", OriginalURL: "https://videos.pexels.com/1.mp4"}
				a, err := librarydomain.NewHarvested(id.New(), ws, uuid.Nil, librarydomain.KindVideo, librarydomain.OriginPexels, h,
					librarydomain.Saved{SHA256: sha, Size: 5, Media: librarydomain.Media{Format: "webm", Width: 1080, Height: 1920, Duration: time.Second}},
					librarydomain.Details{Title: "Sea", Credit: librarydomain.Credit{Author: "Ana"}}, false, at)
				if err != nil {
					t.Fatal(err)
				}
				return a
			}
			first, second := harvested("pexels:video:1", "a", now), harvested("pexels:video:2", "b", now.Add(time.Minute))
			for _, a := range []*librarydomain.Asset{first, second} {
				if err := repo.Create(ctx, a); err != nil {
					t.Fatal(err)
				}
			}
			if err := repo.Create(ctx, harvested("pexels:video:1", "c", now)); !apperror.Is(err, apperror.Conflict) {
				t.Fatalf("same source twice err = %v", err)
			}
			got, err := repo.FindBySourceRef(ctx, ws, "pexels:video:1")
			if err != nil || !reflect.DeepEqual(got.Snapshot(), first.Snapshot()) {
				t.Fatalf("find by source = %+v, %v\nwant %+v", got.Snapshot(), err, first.Snapshot())
			}
			if _, err := repo.FindBySourceRef(ctx, ws, "pexels:video:9"); !apperror.Is(err, apperror.AssetNotFound) {
				t.Fatalf("unknown source err = %v", err)
			}
			list, err := repo.Provisional(ctx, ws, deliverable)
			if err != nil || len(list) != 2 || list[0].ID() != first.ID() {
				t.Fatalf("provisional = %v, %v", list, err)
			}
			first.Keep(now)
			if err := repo.Update(ctx, first); err != nil {
				t.Fatal(err)
			}
			if list, _ := repo.Provisional(ctx, ws, deliverable); len(list) != 1 || list[0].ID() != second.ID() {
				t.Fatalf("provisional after keeping one = %v", list)
			}
			if got, _ := repo.Get(ctx, ws, first.ID()); !got.Kept() || got.Provisional() {
				t.Fatalf("kept = %+v", got.Snapshot())
			}

			if c, err := repo.GetCollect(ctx, ws); err != nil || c.Any() {
				t.Fatalf("default collect = %+v, %v", c, err)
			}
			for _, c := range []librarydomain.Collect{{Music: true, Photo: true}, {Video: true}} {
				if err := repo.SaveCollect(ctx, ws, c, now); err != nil {
					t.Fatal(err)
				}
				if got, err := repo.GetCollect(ctx, ws); err != nil || got != c {
					t.Fatalf("collect = %+v, %v; want %+v", got, err, c)
				}
			}
		})
	}
}
