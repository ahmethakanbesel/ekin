package library_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"ekin/internal/apperror"
	library "ekin/internal/domain/library/v1"
	"ekin/pkg/id"
)

var now = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

func upload(t *testing.T, kind library.Kind, filename string, d library.Details) *library.Asset {
	t.Helper()
	a, err := library.NewUpload(id.New(), id.New(), id.New(), kind, library.Upload{Filename: filename, Size: 10, SHA256: "abc"}, d, now)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestKindFromFilename(t *testing.T) {
	tests := map[string]library.Kind{
		"a.MP4": library.KindVideo, "b.mov": library.KindVideo, "c.webm": library.KindVideo,
		"d.jpeg": library.KindPhoto, "e.png": library.KindPhoto, "f.webp": library.KindPhoto,
		"g.mp3": library.KindMusic, "h.flac": library.KindMusic, "i.opus": library.KindMusic,
	}
	for name, want := range tests {
		if got, err := library.KindFromFilename(name); err != nil || got != want {
			t.Errorf("%s = %s, %v", name, got, err)
		}
	}
	for _, name := range []string{"x.gif", "y.pdf", "noext"} {
		if _, err := library.KindFromFilename(name); !apperror.Is(err, apperror.ValidationFailed) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
}

func TestNewUpload(t *testing.T) {
	tests := []struct {
		name     string
		kind     library.Kind
		filename string
		size     int64
		details  library.Details
		wantErr  bool
		want     library.Kind
	}{
		{name: "kind from extension", filename: "clips/Deniz.mp4", size: 10, want: library.KindVideo},
		{name: "kind given", kind: library.KindMusic, filename: "a.mp3", size: 10, want: library.KindMusic},
		{name: "kind disagrees with file", kind: library.KindPhoto, filename: "a.mp3", size: 10, wantErr: true},
		{name: "empty", filename: "a.jpg", size: 0, wantErr: true},
		{name: "photo too large", filename: "a.jpg", size: library.MaxPhotoBytes + 1, wantErr: true},
		{name: "video at its limit", filename: "a.mov", size: library.MaxVideoBytes, want: library.KindVideo},
		{name: "mood on a photo", filename: "a.jpg", size: 10, details: library.Details{Mood: "calm"}, wantErr: true},
		{name: "tempo out of range", filename: "a.mp3", size: 10, details: library.Details{Tempo: 400}, wantErr: true},
		{name: "credit url not http", filename: "a.mp3", size: 10, details: library.Details{Credit: library.Credit{URL: "ftp://x"}}, wantErr: true},
		{name: "too many tags", filename: "a.mp3", size: 10, details: library.Details{Tags: strings.Fields(strings.Repeat("t ", 21) + "a b c d e f g h i j k l m n o p q r s t u")}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := library.NewUpload(id.New(), id.New(), id.New(), tt.kind,
				library.Upload{Filename: tt.filename, Size: tt.size, SHA256: "abc"}, tt.details, now)
			if tt.wantErr {
				if !apperror.Is(err, apperror.ValidationFailed) {
					t.Fatalf("err = %v, want ValidationFailed", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if a.Kind() != tt.want || a.Origin() != library.OriginUpload || a.Status() != library.StatusProcessing {
				t.Fatalf("asset = %+v", a.Snapshot())
			}
		})
	}
}

func TestDescribe(t *testing.T) {
	a := upload(t, "", "dir/Sakin Sabah.mp3", library.Details{})
	if a.Title() != "Sakin Sabah" || a.Filename() != "Sakin Sabah.mp3" {
		t.Fatalf("defaults = %q %q", a.Title(), a.Filename())
	}
	err := a.Describe(library.Details{
		Title: "  Sakin sabah ", Tags: []string{" Piano ", "piano", "", "Lo  Fi"}, Mood: "calm", Tempo: 80,
		Credit: library.Credit{Author: " Ayşe ", URL: "https://example.com/a"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if a.Title() != "Sakin sabah" || !reflect.DeepEqual(a.Tags(), []string{"piano", "lo fi"}) || a.Credit().Author != "Ayşe" {
		t.Fatalf("described = %+v", a.Snapshot())
	}
	if err := a.Describe(library.Details{}, now); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("empty title accepted: %v", err)
	}
}

func TestIngested(t *testing.T) {
	tests := []struct {
		name       string
		filename   string
		media      library.Media
		wantStatus library.Status
		wantErr    apperror.Code
	}{
		{name: "video", filename: "a.mp4", media: library.Media{Format: "webm", Width: 1080, Height: 1920, Duration: 12 * time.Second, Poster: true}, wantStatus: library.StatusReady},
		{name: "video too long", filename: "a.mp4", media: library.Media{Format: "webm", Width: 1080, Height: 1920, Duration: 3 * time.Minute}, wantStatus: library.StatusFailed},
		{name: "video without picture", filename: "a.mp4", media: library.Media{Format: "webm", Duration: time.Second}, wantStatus: library.StatusFailed},
		{name: "photo", filename: "a.png", media: library.Media{Format: "jpg", Width: 2000, Height: 1000, Duration: time.Second}, wantStatus: library.StatusReady},
		{name: "music", filename: "a.mp3", media: library.Media{Format: "opus", Duration: 3 * time.Minute, Width: 5, Height: 5}, wantStatus: library.StatusReady},
		{name: "music too long", filename: "a.mp3", media: library.Media{Format: "opus", Duration: 20 * time.Minute}, wantStatus: library.StatusFailed},
		{name: "music without length", filename: "a.mp3", media: library.Media{Format: "opus"}, wantStatus: library.StatusFailed},
		{name: "wrong format", filename: "a.mp3", media: library.Media{Format: "webm", Duration: time.Second}, wantErr: apperror.InternalError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := upload(t, "", tt.filename, library.Details{})
			err := a.Ingested(tt.media, now)
			if tt.wantErr != "" {
				if !apperror.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %s", err, tt.wantErr)
				}
				return
			}
			if err != nil || a.Status() != tt.wantStatus {
				t.Fatalf("status = %s, %v; failure %q", a.Status(), err, a.Failure())
			}
		})
	}
	a := upload(t, "", "a.mp3", library.Details{})
	_ = a.Ingested(library.Media{Format: "opus", Duration: time.Minute, Width: 5, Height: 5, Poster: true}, now)
	if a.Width() != 0 || a.HasPoster() || a.Orientation() != "" {
		t.Fatalf("music kept a picture: %+v", a.Snapshot())
	}
	if err := a.Ingested(library.Media{Format: "opus", Duration: time.Minute}, now); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("ingested twice: %v", err)
	}
}

func TestRetryAndArchive(t *testing.T) {
	a := upload(t, "", "a.mp4", library.Details{})
	if err := a.Retry(now); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("retried while processing: %v", err)
	}
	_ = a.Failed(strings.Repeat("x", 3000), now)
	if a.Usable() || len(a.Failure()) != 2000 {
		t.Fatalf("failed = %s / %d", a.Status(), len(a.Failure()))
	}
	if err := a.Retry(now); err != nil || a.Status() != library.StatusProcessing || a.Failure() != "" {
		t.Fatalf("retry = %v, %+v", err, a.Snapshot())
	}
	_ = a.Ingested(library.Media{Format: "webm", Width: 1920, Height: 1080, Duration: time.Second}, now)
	if !a.Usable() {
		t.Fatal("ready asset not usable")
	}
	a.Archive(now.Add(time.Hour))
	if a.Usable() || !a.ArchivedAt().Equal(now.Add(time.Hour)) {
		t.Fatalf("archived = %v", a.ArchivedAt())
	}
	a.Archive(now.Add(2 * time.Hour))
	if !a.ArchivedAt().Equal(now.Add(time.Hour)) {
		t.Fatal("archiving again moved the date")
	}
	a.Unarchive(now)
	if !a.Usable() {
		t.Fatal("unarchived asset not usable")
	}
}

func TestFits(t *testing.T) {
	tests := []struct {
		width, height int
		frame         library.Orientation
		want          bool
	}{
		{1080, 1920, library.OrientationPortrait, true},
		{1080, 1920, library.OrientationLandscape, false},
		{1920, 1080, library.OrientationLandscape, true},
		{1000, 1030, library.OrientationPortrait, true}, // within 5%: square
		{1000, 1030, library.OrientationLandscape, true},
		{1000, 1100, library.OrientationLandscape, false},
	}
	for _, tt := range tests {
		a := upload(t, "", "a.jpg", library.Details{})
		_ = a.Ingested(library.Media{Format: "jpg", Width: tt.width, Height: tt.height}, now)
		if got := a.Fits(tt.frame); got != tt.want {
			t.Errorf("%dx%d in %s = %v", tt.width, tt.height, tt.frame, got)
		}
	}
	m := upload(t, "", "a.mp3", library.Details{})
	if !m.Fits(library.OrientationLandscape) {
		t.Error("music does not fit")
	}
}

func TestKeys(t *testing.T) {
	a := upload(t, "", "Clip.MOV", library.Details{})
	if !strings.HasSuffix(a.OriginalKey(), "/original.mov") || !strings.HasPrefix(a.OriginalKey(), a.Prefix()) {
		t.Fatalf("original = %s", a.OriginalKey())
	}
	if _, ok := a.Key(library.FileMain); ok {
		t.Fatal("main file before ingestion")
	}
	_ = a.Ingested(library.Media{Format: "webm", Width: 1920, Height: 1080, Duration: time.Second, Poster: true}, now)
	main, _ := a.Key(library.FileMain)
	poster, ok := a.Key(library.FilePoster)
	if main != a.Prefix()+"/main.webm" || !ok || poster != a.Prefix()+"/poster.jpg" || a.ContentType(library.FileMain) != "video/webm" {
		t.Fatalf("keys = %s %s %s", main, poster, a.ContentType(library.FileMain))
	}
	if _, err := library.ParseFile("original"); !apperror.Is(err, apperror.AssetNotFound) {
		t.Fatalf("original served: %v", err)
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	a := upload(t, "", "a.mp3", library.Details{Tags: []string{"calm"}, Mood: "calm", Tempo: 70, Credit: library.Credit{Author: "x"}})
	_ = a.Ingested(library.Media{Format: "opus", Duration: 90 * time.Second}, now)
	a.Archive(now)
	b, err := library.Rehydrate(a.Snapshot())
	if err != nil || !reflect.DeepEqual(b.Snapshot(), a.Snapshot()) {
		t.Fatalf("round trip = %+v, %v", b, err)
	}
	s := a.Snapshot()
	s.Format = "exe"
	if _, err := library.Rehydrate(s); !apperror.Is(err, apperror.InternalError) {
		t.Fatalf("bad format rehydrated: %v", err)
	}
}
