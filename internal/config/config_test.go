package config

import (
	"strings"
	"testing"
	"time"

	jobdomain "kurgu/internal/domain/job/v1"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "127.0.0.1:8080" || cfg.DBPath != "data/kurgu.db" || !cfg.MigrateOnStart {
		t.Fatalf("defaults = %+v", cfg)
	}
	if cfg.Worker.Concurrency[jobdomain.QueueIO] != 2 || cfg.Worker.Concurrency[jobdomain.QueueRender] != 1 {
		t.Fatalf("default concurrency = %v", cfg.Worker.Concurrency)
	}
	if !cfg.Worker.HeavyExclusive || cfg.Worker.Cooldown != 30*time.Second {
		t.Fatalf("default throttle = %+v", cfg.Worker)
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"bad bool", map[string]string{"KURGU_WORKER_PAUSED": "maybe"}, "KURGU_WORKER_PAUSED"},
		{"bad duration", map[string]string{"KURGU_HEAVY_COOLDOWN": "soon"}, "KURGU_HEAVY_COOLDOWN"},
		{"tiny tick", map[string]string{"KURGU_SCHEDULER_TICK": "10ms"}, "KURGU_SCHEDULER_TICK"},
		{"bad log format", map[string]string{"KURGU_LOG_FORMAT": "xml"}, "KURGU_LOG_FORMAT"},
		{"bad concurrency", map[string]string{"KURGU_QUEUE_CONCURRENCY": "gpu=1"}, "KURGU_QUEUE_CONCURRENCY"},
		{"unknown blob driver", map[string]string{"KURGU_BLOB_DRIVER": "gcs"}, "KURGU_BLOB_DRIVER"},
		{"s3 without bucket", map[string]string{"KURGU_BLOB_DRIVER": "s3", "KURGU_S3_ENDPOINT": "https://x"}, "KURGU_S3_BUCKET"},
		{"relative public url", map[string]string{"KURGU_PUBLIC_URL": "kurgu.example.com"}, "KURGU_PUBLIC_URL"},
		{"public url with path", map[string]string{"KURGU_PUBLIC_URL": "https://example.com/kurgu"}, "KURGU_PUBLIC_URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := load(env(tt.env))
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
		{map[string]string{"KURGU_HTTP_ADDR": ":9000"}, "http://localhost:9000"},
		{map[string]string{"KURGU_HTTP_ADDR": "0.0.0.0:8080"}, "http://localhost:8080"},
		{map[string]string{"KURGU_PUBLIC_URL": "https://kurgu.example.com/"}, "https://kurgu.example.com"},
	}
	for _, tt := range tests {
		cfg, err := load(env(tt.env))
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
