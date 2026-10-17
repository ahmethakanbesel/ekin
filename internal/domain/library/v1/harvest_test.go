package library_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	library "ekin/internal/domain/library/v1"
	"ekin/pkg/id"
)

func harvest() library.Harvest {
	return library.Harvest{StoryID: id.New(), DeliverableID: id.New(), Revision: 2}
}

var clip = library.Saved{SHA256: "abc", Size: 10, Media: library.Media{Format: "webm", Width: 1080, Height: 1920, Duration: 12 * time.Second, Poster: true}}

func TestNewHarvested(t *testing.T) {
	pexels := harvest()
	pexels.SourceRef, pexels.OriginalURL = "pexels:video:42", "https://videos.pexels.com/42.mp4"
	score := library.Saved{SHA256: "s", Size: 300, Media: library.Media{Format: "js"}}
	tests := []struct {
		name    string
		kind    library.Kind
		origin  library.Origin
		h       library.Harvest
		f       library.Saved
		d       library.Details
		wantErr bool
	}{
		{name: "pexels clip", kind: library.KindVideo, origin: library.OriginPexels, h: pexels, f: clip,
			d: library.Details{Title: "Sea", Credit: library.Credit{Author: "Ana"}}},
		{name: "composed score", kind: library.KindMusic, origin: library.OriginGenerated, h: harvest(), f: score,
			d: library.Details{Title: "Calm", Mood: "calm"}},
		{name: "pexels without id", kind: library.KindVideo, origin: library.OriginPexels, h: harvest(), f: clip,
			d: library.Details{Title: "Sea", Credit: library.Credit{Author: "Ana"}}, wantErr: true},
		{name: "pexels without author", kind: library.KindVideo, origin: library.OriginPexels, h: pexels, f: clip,
			d: library.Details{Title: "Sea"}, wantErr: true},
		{name: "composed video", kind: library.KindVideo, origin: library.OriginGenerated, h: harvest(), f: clip,
			d: library.Details{Title: "x"}, wantErr: true},
		{name: "upload", kind: library.KindVideo, origin: library.OriginUpload, h: harvest(), f: clip,
			d: library.Details{Title: "x"}, wantErr: true},
		{name: "no revision", kind: library.KindMusic, origin: library.OriginGenerated, h: library.Harvest{StoryID: id.New(), DeliverableID: id.New()},
			f: score, d: library.Details{Title: "x"}, wantErr: true},
		{name: "too long", kind: library.KindVideo, origin: library.OriginPexels, h: pexels,
			f: library.Saved{SHA256: "a", Size: 1, Media: library.Media{Format: "webm", Width: 1, Height: 1, Duration: time.Hour}},
			d: library.Details{Title: "Sea", Credit: library.Credit{Author: "Ana"}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := library.NewHarvested(id.New(), id.New(), uuid.Nil, tt.kind, tt.origin, tt.h, tt.f, tt.d, false, now)
			if tt.wantErr {
				if !apperror.Is(err, apperror.ValidationFailed) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !a.Provisional() || a.Kept() || a.Status() != library.StatusReady || a.Harvest() != tt.h || a.Origin() != tt.origin {
				t.Fatalf("harvested = %+v", a.Snapshot())
			}
		})
	}
	a, err := library.NewHarvested(id.New(), id.New(), id.New(), library.KindVideo, library.OriginPexels, pexels, clip,
		library.Details{Title: "Sea", Credit: library.Credit{Author: "Ana"}}, true, now)
	if err != nil || a.Provisional() || !a.Kept() {
		t.Fatalf("saved by a person = %+v, %v", a, err)
	}
}

func TestSettle(t *testing.T) {
	newAsset := func() *library.Asset {
		h := harvest()
		h.SourceRef = "pexels:video:1"
		a, err := library.NewHarvested(id.New(), id.New(), uuid.Nil, library.KindVideo, library.OriginPexels, h, clip,
			library.Details{Title: "Sea", Credit: library.Credit{Author: "Ana"}}, false, now)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	tests := []struct {
		name                         string
		prepare                      func(a *library.Asset)
		usedByDecided, usedElsewhere bool
		want                         bool
	}{
		{name: "replaced by a later revision", want: false},
		{name: "used by the decided revision", usedByDecided: true, want: true},
		{name: "used by another video", usedElsewhere: true, want: true},
		{name: "kept by a person", prepare: func(a *library.Asset) { a.Keep(now) }, want: true},
		{name: "described by a person", prepare: func(a *library.Asset) {
			_ = a.Describe(library.Details{Title: "Renamed", Credit: library.Credit{Author: "Ana"}}, now)
		}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newAsset()
			if tt.prepare != nil {
				tt.prepare(a)
			}
			if got := a.Settle(tt.usedByDecided, tt.usedElsewhere, now); got != tt.want {
				t.Fatalf("settle = %v, want %v", got, tt.want)
			}
			if tt.want && a.Provisional() {
				t.Fatal("still provisional after settling")
			}
		})
	}
	up := upload(t, "", "a.mp4", library.Details{})
	if !up.Kept() || up.Provisional() || !up.Settle(false, false, now) {
		t.Fatal("an upload is pruned")
	}
}

func TestCollect(t *testing.T) {
	if (library.Collect{}).Any() {
		t.Fatal("zero collects")
	}
	c := library.Collect{Video: true}
	if !c.Any() || !c.Collects(library.KindVideo) || c.Collects(library.KindMusic) || c.Collects(library.KindPhoto) {
		t.Fatalf("collect = %+v", c)
	}
}

func TestScorePreview(t *testing.T) {
	a, err := library.NewHarvested(id.New(), id.New(), uuid.Nil, library.KindMusic, library.OriginGenerated, harvest(),
		library.Saved{SHA256: "s", Size: 3, Media: library.Media{Format: "js"}}, library.Details{Title: "Calm"}, false, now)
	if err != nil {
		t.Fatal(err)
	}
	main, _ := a.Key(library.FileMain)
	preview, ok := a.Key(library.FilePreview)
	if main != a.Prefix()+"/main.js" || !ok || preview != a.Prefix()+"/preview.opus" || a.ContentType(library.FilePreview) != "audio/ogg" {
		t.Fatalf("keys = %s %s", main, preview)
	}
	track := upload(t, "", "a.mp3", library.Details{})
	_ = track.Ingested(library.Media{Format: "opus", Duration: time.Minute}, now)
	if _, ok := track.Key(library.FilePreview); ok {
		t.Fatal("a track has a preview of its own")
	}
}
