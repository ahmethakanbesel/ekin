package config

import (
	"slices"
	"strings"
	"testing"
	"time"

	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
)

func vars(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(vars(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "127.0.0.1:8080" || cfg.DBPath != "data/kurgu.db" || !cfg.MigrateOnStart {
		t.Fatalf("defaults = %+v", cfg)
	}
	if cfg.Worker.Concurrency[jobdomain.QueueIO] != 2 || cfg.Worker.Concurrency[jobdomain.QueueRender] != 1 {
		t.Fatalf("default concurrency = %v", cfg.Worker.Concurrency)
	}
	if !cfg.Worker.HeavyExclusive || cfg.Worker.Cooldown != 30*time.Second || cfg.Worker.HeavyQueues != nil || cfg.Worker.Nice != 0 {
		t.Fatalf("default throttle = %+v", cfg.Worker)
	}
	if cfg.Production.PreviewFPS != 30 || cfg.Production.FinalFPS != 30 {
		t.Fatalf("default frame rates = %d, %d", cfg.Production.PreviewFPS, cfg.Production.FinalFPS)
	}
	cfg, err = load(vars(map[string]string{"EKIN_RENDER_PREVIEW_FPS": "15", "EKIN_RENDER_FINAL_FPS": "60"}))
	if err != nil || cfg.Production.PreviewFPS != 15 || cfg.Production.FinalFPS != 60 {
		t.Fatalf("frame rates = %d, %d (%v)", cfg.Production.PreviewFPS, cfg.Production.FinalFPS, err)
	}
}

func TestLoadHeavyQueuesAndNice(t *testing.T) {
	tests := []struct {
		raw  string
		want []jobdomain.Queue
	}{
		{"tts, render", []jobdomain.Queue{jobdomain.QueueTTS, jobdomain.QueueRender}},
		{"none", []jobdomain.Queue{}},
	}
	for _, tt := range tests {
		cfg, err := load(vars(map[string]string{"EKIN_HEAVY_QUEUES": tt.raw, "EKIN_NICE": "10"}))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Worker.HeavyQueues == nil || !slices.Equal(cfg.Worker.HeavyQueues, tt.want) || cfg.Worker.Nice != 10 {
			t.Fatalf("%q: heavy %v, nice %d", tt.raw, cfg.Worker.HeavyQueues, cfg.Worker.Nice)
		}
	}
}

func TestLoadShortsLimits(t *testing.T) {
	cfg, err := load(vars(nil))
	if err != nil {
		t.Fatal(err)
	}
	if l := cfg.Shorts; l.SourceMin() != 4*time.Minute || l.SourceMax() != time.Hour || l.ClipMin() != 20*time.Second ||
		l.ClipMax() != time.Minute || l.MaxPerVideo() != 5 || l.MinGap() != 30*time.Second {
		t.Fatalf("default shorts limits = %+v", l)
	}
	cfg, err = load(vars(map[string]string{
		"EKIN_SHORTS_SOURCE_MIN": "8m", "EKIN_SHORTS_SOURCE_MAX": "30m", "EKIN_SHORTS_CLIP_MIN": "15s",
		"EKIN_SHORTS_CLIP_MAX": "90s", "EKIN_SHORTS_MAX_PER_VIDEO": "3", "EKIN_SHORTS_MIN_GAP": "0s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if l := cfg.Shorts; l.SourceMin() != 8*time.Minute || l.SourceMax() != 30*time.Minute || l.ClipMin() != 15*time.Second ||
		l.ClipMax() != 90*time.Second || l.MaxPerVideo() != 3 || l.MinGap() != 0 {
		t.Fatalf("shorts limits = %+v", l)
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"bad bool", map[string]string{"EKIN_WORKER_PAUSED": "maybe"}, "EKIN_WORKER_PAUSED"},
		{"bad duration", map[string]string{"EKIN_HEAVY_COOLDOWN": "soon"}, "EKIN_HEAVY_COOLDOWN"},
		{"tiny tick", map[string]string{"EKIN_SCHEDULER_TICK": "10ms"}, "EKIN_SCHEDULER_TICK"},
		{"bad log format", map[string]string{"EKIN_LOG_FORMAT": "xml"}, "EKIN_LOG_FORMAT"},
		{"bad concurrency", map[string]string{"EKIN_QUEUE_CONCURRENCY": "gpu=1"}, "EKIN_QUEUE_CONCURRENCY"},
		{"bad heavy queue", map[string]string{"EKIN_HEAVY_QUEUES": "render,gpu"}, "EKIN_HEAVY_QUEUES"},
		{"nice too low", map[string]string{"EKIN_NICE": "20"}, "EKIN_NICE"},
		{"bad claude effort", map[string]string{"EKIN_CLAUDE_EFFORT": "turbo"}, "EKIN_CLAUDE_EFFORT"},
		{"unknown blob driver", map[string]string{"EKIN_BLOB_DRIVER": "gcs"}, "EKIN_BLOB_DRIVER"},
		{"s3 without bucket", map[string]string{"EKIN_BLOB_DRIVER": "s3", "EKIN_S3_ENDPOINT": "https://x"}, "EKIN_S3_BUCKET"},
		{"relative public url", map[string]string{"EKIN_PUBLIC_URL": "ekin.example.com"}, "EKIN_PUBLIC_URL"},
		{"public url with path", map[string]string{"EKIN_PUBLIC_URL": "https://example.com/ekin"}, "EKIN_PUBLIC_URL"},
		{"bad shorts duration", map[string]string{"EKIN_SHORTS_SOURCE_MIN": "long"}, "EKIN_SHORTS_SOURCE_MIN"},
		{"shorts source minimum above maximum", map[string]string{"EKIN_SHORTS_SOURCE_MIN": "90m"}, "EKIN_SHORTS_*"},
		{"short over 3 minutes", map[string]string{"EKIN_SHORTS_CLIP_MAX": "4m"}, "EKIN_SHORTS_*"},
		{"too many shorts per video", map[string]string{"EKIN_SHORTS_MAX_PER_VIDEO": "21"}, "EKIN_SHORTS_MAX_PER_VIDEO"},
		{"preview fps in range but not offered", map[string]string{"EKIN_RENDER_PREVIEW_FPS": "25"}, "EKIN_RENDER_PREVIEW_FPS"},
		{"preview fps above 30", map[string]string{"EKIN_RENDER_PREVIEW_FPS": "60"}, "EKIN_RENDER_PREVIEW_FPS"},
		{"final fps not offered", map[string]string{"EKIN_RENDER_FINAL_FPS": "48"}, "EKIN_RENDER_FINAL_FPS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := load(vars(tt.env))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want mention of %s", err, tt.want)
			}
		})
	}
}

func TestPublicURL(t *testing.T) {
	tests := []struct {
		env  map[string]string
		want string
	}{
		{nil, "http://127.0.0.1:8080"},
		{map[string]string{"EKIN_HTTP_ADDR": ":9000"}, "http://localhost:9000"},
		{map[string]string{"EKIN_HTTP_ADDR": "0.0.0.0:8080"}, "http://localhost:8080"},
		{map[string]string{"EKIN_PUBLIC_URL": "https://ekin.example.com/"}, "https://ekin.example.com"},
	}
	for _, tt := range tests {
		cfg, err := load(vars(tt.env))
		if err != nil || cfg.PublicURL != tt.want {
			t.Errorf("env %v: public url = %q, %v; want %q", tt.env, cfg.PublicURL, err, tt.want)
		}
	}
}

func TestParseConcurrency(t *testing.T) {
	tests := []struct {
		raw     string
		want    map[jobdomain.Queue]int
		wantErr bool
	}{
		{raw: "", want: map[jobdomain.Queue]int{}},
		{raw: "render=0, agent=2", want: map[jobdomain.Queue]int{jobdomain.QueueRender: 0, jobdomain.QueueAgent: 2}},
		{raw: "io=1,", want: map[jobdomain.Queue]int{jobdomain.QueueIO: 1}},
		{raw: "io", wantErr: true},
		{raw: "io=-1", wantErr: true},
		{raw: "io=17", wantErr: true},
		{raw: "io=1,io=2", wantErr: true},
		{raw: "gpu=1", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := ParseConcurrency(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for q, n := range tt.want {
				if got[q] != n {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}
