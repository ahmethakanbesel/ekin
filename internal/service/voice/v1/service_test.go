package voice_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ahmethakanbesel/ekin/internal/adapter/elevenlabs"
	"github.com/ahmethakanbesel/ekin/internal/adapter/speech"
	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/blob"
	"github.com/ahmethakanbesel/ekin/internal/db/dbtest"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	voicedomain "github.com/ahmethakanbesel/ekin/internal/domain/voice/v1"
	workspacedomain "github.com/ahmethakanbesel/ekin/internal/domain/workspace/v1"
	jobrepo "github.com/ahmethakanbesel/ekin/internal/repository/job/v1"
	settingsrepo "github.com/ahmethakanbesel/ekin/internal/repository/settings/v1"
	workspacerepo "github.com/ahmethakanbesel/ekin/internal/repository/workspace/v1"
	jobsvc "github.com/ahmethakanbesel/ekin/internal/service/job/v1"
	voicesvc "github.com/ahmethakanbesel/ekin/internal/service/voice/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

type speaker struct{ got speech.Options }

func (s *speaker) Say(_ context.Context, _, _, outDir string, o speech.Options) (speech.Timings, error) {
	s.got = o
	return speech.Timings{Duration: 1}, os.WriteFile(filepath.Join(outDir, "speech.wav"), []byte("RIFF-preview"), 0o600)
}

func TestVoiceSettingsAndPreview(t *testing.T) {
	ctx := context.Background()
	_, tx := dbtest.Open(t)
	w, _ := workspacedomain.New(id.New(), "ws", time.UTC, time.Now())
	_ = workspacerepo.NewSQLite(tx).Create(ctx, w)
	jobs := jobsvc.NewService(jobrepo.NewSQLite(tx), tx)
	blobs, _ := blob.NewFS(t.TempDir())
	sp := &speaker{}
	svc := voicesvc.NewService(settingsrepo.NewSQLite(tx), jobs, sp, blobs)

	v, _ := svc.Get(ctx, w.ID())
	if v.Languages["en"].Voice != "af_heart" {
		t.Fatalf("defaults = %+v", v)
	}
	v.Languages["tr"] = voicedomain.Config{Voice: "okur", Speed: 1.1, SentencePause: 0.3, ParagraphPause: 0.8, Seed: 7}
	v.Lexicon = map[string]map[string]string{"tr": {"acme": "ek mi"}}
	if _, err := svc.Save(ctx, w.ID(), id.New(), v); err != nil {
		t.Fatal(err)
	}
	o, err := svc.Options(ctx, w.ID(), "tr", &voicedomain.Override{Speed: 0.9})
	if err != nil || o.Speed != 0.9 || o.Seed != 7 || o.ParagraphPause != 0.8 || o.Lexicon["acme"] != "ek mi" {
		t.Fatalf("options = %+v %v", o, err)
	}
	v.Languages["en"] = voicedomain.Config{Voice: "nobody", Speed: 1}
	if _, err := svc.Save(ctx, w.ID(), id.New(), v); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("unknown voice saved: %v", err)
	}

	pid, _, err := svc.QueuePreview(ctx, w.ID(), "tr", voicedomain.Config{Voice: "okur", Speed: 1.2}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.PreviewAudio(ctx, w.ID(), pid); !apperror.Is(err, apperror.NotFound) {
		t.Fatalf("preview ready before the job ran: %v", err)
	}
	j, _ := jobs.Claim(ctx, []jobdomain.Queue{jobdomain.QueueTTS}, []string{voicesvc.KindPreview}, "t", time.Minute)
	var p voicesvc.PreviewPayload
	_ = json.Unmarshal(j.Payload(), &p)
	if err := svc.Preview(ctx, p); err != nil {
		t.Fatal(err)
	}
	if sp.got.Speed != 1.2 || sp.got.Lexicon["acme"] != "ek mi" {
		t.Fatalf("preview options = %+v", sp.got)
	}
	rc, _, err := svc.PreviewAudio(ctx, w.ID(), pid)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(b) != "RIFF-preview" {
		t.Fatalf("preview = %q", b)
	}
	if _, _, err := svc.QueuePreview(ctx, w.ID(), "en", voicedomain.Config{Voice: "okur", Speed: 1}, ""); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("okur accepted for English: %v", err)
	}
}

func TestElevenLabsNeedsAKey(t *testing.T) {
	ctx := context.Background()
	_, tx := dbtest.Open(t)
	w, _ := workspacedomain.New(id.New(), "ws", time.UTC, time.Now())
	_ = workspacerepo.NewSQLite(tx).Create(ctx, w)
	jobs := jobsvc.NewService(jobrepo.NewSQLite(tx), tx)
	blobs, _ := blob.NewFS(t.TempDir())
	key := ""
	svc := voicesvc.NewService(settingsrepo.NewSQLite(tx), jobs, &speaker{}, blobs).
		WithElevenLabs(func(context.Context) *elevenlabs.Client { return &elevenlabs.Client{APIKey: key} })

	v, _ := svc.Get(ctx, w.ID())
	v.Languages["en"] = voicedomain.Config{Engine: voicedomain.EngineElevenLabs, Voice: "21m00Tcm4TlvDq8ikWAM", Speed: 1}
	cfg := v.Languages["en"]
	override := voicedomain.Override{Engine: voicedomain.EngineElevenLabs}
	if _, err := svc.Save(ctx, w.ID(), id.New(), v); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("saved without a key: %v", err)
	}
	if err := svc.CheckOverride(ctx, "en", override); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("override accepted without a key: %v", err)
	}
	if _, _, err := svc.QueuePreview(ctx, w.ID(), "en", cfg, ""); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("preview queued without a key: %v", err)
	}
	key = "sk_test"
	if _, err := svc.Save(ctx, w.ID(), id.New(), v); err != nil {
		t.Fatal(err)
	}
	if err := svc.CheckOverride(ctx, "en", override); err != nil {
		t.Fatal(err)
	}
	o, err := svc.Options(ctx, w.ID(), "en", nil)
	if err != nil || o.Engine != "elevenlabs" || o.ElevenLabs == nil || o.ElevenLabs.Model != "eleven_multilingual_v2" {
		t.Fatalf("options = %+v, %v", o, err)
	}
}
