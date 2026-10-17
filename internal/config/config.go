// Package config loads process configuration from KURGU_* environment
// variables. Every setting has a default that works for local
// development on a laptop.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	jobdomain "kurgu/internal/domain/job/v1"
)

// Config is the process configuration.
type Config struct {
	HTTPAddr string
	// PublicURL is the origin browsers use (invitation links, cookie
	// Secure flag, trusted origin). Defaults to http://<HTTPAddr>.
	PublicURL      string
	DataDir        string
	DBPath         string
	MigrateOnStart bool
	SchedulerTick  time.Duration
	LogFormat      string // text | json
	LogLevel       string // debug | info | warn | error
	Worker         Worker
	Blob           Blob
	Tools          Tools
	Production     Production
	Notify         Notify
	Publishing     Publishing
	Remote         Remote
}

// Remote configures the hybrid-worker API on both ends: the server
// accepts workers presenting Token; a worker with URL set claims jobs
// from that server instead of opening a database.
type Remote struct {
	URL   string
	Token string
}

// Publishing configures YouTube OAuth and the secret key.
type Publishing struct {
	YouTubeClientID, YouTubeClientSecret string
	// SecretKeyFile holds the key that seals stored tokens; created on
	// first start. KURGU_SECRET_KEY overrides it.
	SecretKeyFile, SecretKey string
}

// Notify configures email and decisions.
type Notify struct {
	SMTPHost, SMTPUser, SMTPPassword, SMTPFrom, SMTPTLS string
	SMTPPort                                            int
	Every                                               time.Duration
	OpenRouterKey, DecisionModel                        string
}

// Production configures the authoring agent, speech and rendering.
type Production struct {
	WorkDir      string
	ClaudeBin    string
	ClaudeModel  string
	ClaudeEffort string // --effort for every agent run; empty for the CLI's default
	AgentTimeout time.Duration
	SpeechDir    string
	SpeechModels string
	UV           string
	PexelsKey    string
	// ElevenLabsKey is the ElevenLabs API key; on a hybrid worker it is the
	// only source, since the worker has no settings database.
	ElevenLabsKey string
	Chrome        string
	FFmpeg        string
	Encoder       string
	Chunks        int
	Duty          float64
	Threads       int
	PreviewScale  float64
	FinalScale    float64
	GPU           bool
}

// Blob selects the blob store.
type Blob struct {
	Driver string // fs | s3
	Dir    string // fs root
	S3     S3
}

// S3 configures the s3 blob driver.
type S3 struct {
	Endpoint, Region, Bucket, Prefix, AccessKey, SecretKey string
}

// Tools are external binaries the worker runs.
type Tools struct {
	PDFToText string
	PDFToHTML string // reads PDF bookmarks as a table of contents
}

// Worker configures the job runner (PLAN.md §4).
type Worker struct {
	Owner          string
	Profile        string // host profile name (runtime throttle settings)
	Concurrency    map[jobdomain.Queue]int
	HeavyExclusive bool
	Cooldown       time.Duration
	Paused         bool
	CPULimit       int               // percent of the host's CPUs for job processes; 0 = none
	HeavyQueues    []jobdomain.Queue // nil = jobdomain.DefaultHeavy
	Nice           int               // job processes' priority, 0 (normal) to 19
	Lease          time.Duration
}

// DefaultConcurrency suits the 2 vCPU VPS: one of everything, two for
// network-bound io.
const DefaultConcurrency = "control=1,agent=1,tts=1,render=1,extract=1,io=2"

// Load reads the environment.
func Load() (Config, error) {
	return load(os.Getenv)
}

// Defaults that the settings page reports as built in.
const (
	DefaultDecisionModel = "typesafe/jev-1.13"
	DefaultSMTPTLS       = "starttls"
)

func load(getenv func(string) string) (Config, error) {
	var errs []string
	str := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	boolean := func(key string, def bool) bool {
		raw := strings.TrimSpace(getenv(key))
		if raw == "" {
			return def
		}
		v, err := strconv.ParseBool(raw)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: not a boolean: %q", key, raw))
			return def
		}
		return v
	}
	duration := func(key string, def time.Duration) time.Duration {
		raw := strings.TrimSpace(getenv(key))
		if raw == "" {
			return def
		}
		v, err := time.ParseDuration(raw)
		if err != nil || v < 0 {
			errs = append(errs, fmt.Sprintf("%s: not a non-negative duration: %q", key, raw))
			return def
		}
		return v
	}

	dataDir := str("KURGU_DATA_DIR", "data")
	httpAddr := str("KURGU_HTTP_ADDR", "127.0.0.1:8080")
	cfg := Config{
		HTTPAddr:       httpAddr,
		PublicURL:      strings.TrimRight(str("KURGU_PUBLIC_URL", defaultPublicURL(httpAddr)), "/"),
		DataDir:        dataDir,
		DBPath:         str("KURGU_DB_PATH", filepath.Join(dataDir, "kurgu.db")),
		MigrateOnStart: boolean("KURGU_MIGRATE_ON_START", true),
		SchedulerTick:  duration("KURGU_SCHEDULER_TICK", 30*time.Second),
		LogFormat:      str("KURGU_LOG_FORMAT", "text"),
		LogLevel:       str("KURGU_LOG_LEVEL", "info"),
		Worker: Worker{
			Owner:          str("KURGU_WORKER_OWNER", defaultOwner()),
			Profile:        str("KURGU_WORKER_PROFILE", "default"),
			HeavyExclusive: boolean("KURGU_HEAVY_EXCLUSIVE", true),
			Cooldown:       duration("KURGU_HEAVY_COOLDOWN", 30*time.Second),
			Paused:         boolean("KURGU_WORKER_PAUSED", false),
			Lease:          duration("KURGU_JOB_LEASE", 2*time.Minute),
		},
	}
	cfg.Blob = Blob{
		Driver: str("KURGU_BLOB_DRIVER", "fs"),
		Dir:    str("KURGU_BLOB_DIR", filepath.Join(dataDir, "blobs")),
		S3: S3{
			Endpoint:  str("KURGU_S3_ENDPOINT", ""),
			Region:    str("KURGU_S3_REGION", "auto"),
			Bucket:    str("KURGU_S3_BUCKET", ""),
			Prefix:    str("KURGU_S3_PREFIX", ""),
			AccessKey: str("KURGU_S3_ACCESS_KEY_ID", ""),
			SecretKey: str("KURGU_S3_SECRET_ACCESS_KEY", ""),
		},
	}
	switch cfg.Blob.Driver {
	case "fs":
	case "s3":
		if cfg.Blob.S3.Endpoint == "" || cfg.Blob.S3.Bucket == "" || cfg.Blob.S3.AccessKey == "" || cfg.Blob.S3.SecretKey == "" {
			errs = append(errs, "KURGU_BLOB_DRIVER=s3 needs KURGU_S3_ENDPOINT, KURGU_S3_BUCKET, KURGU_S3_ACCESS_KEY_ID and KURGU_S3_SECRET_ACCESS_KEY")
		}
	default:
		errs = append(errs, "KURGU_BLOB_DRIVER: must be fs or s3")
	}
	cfg.Tools = Tools{PDFToText: str("KURGU_PDFTOTEXT", "pdftotext"), PDFToHTML: str("KURGU_PDFTOHTML", "pdftohtml")}
	number := func(key string, def, lo, hi float64) float64 {
		raw := strings.TrimSpace(getenv(key))
		if raw == "" {
			return def
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v < lo || v > hi {
			errs = append(errs, fmt.Sprintf("%s: must be a number from %g to %g", key, lo, hi))
			return def
		}
		return v
	}
	speechDir, _ := filepath.Abs(str("KURGU_SPEECH_DIR", "tools/speech"))
	cfg.Production = Production{
		WorkDir:       str("KURGU_WORK_DIR", filepath.Join(dataDir, "work")),
		ClaudeBin:     str("KURGU_CLAUDE_BIN", "claude"),
		ClaudeModel:   str("KURGU_CLAUDE_MODEL", ""),
		ClaudeEffort:  str("KURGU_CLAUDE_EFFORT", ""),
		AgentTimeout:  duration("KURGU_AGENT_TIMEOUT", 45*time.Minute),
		SpeechDir:     speechDir,
		SpeechModels:  str("KURGU_SPEECH_MODELS", ""),
		UV:            str("KURGU_UV", "uv"),
		PexelsKey:     str("KURGU_PEXELS_API_KEY", ""),
		ElevenLabsKey: str("KURGU_ELEVENLABS_API_KEY", ""),
		Chrome:        str("KURGU_CHROME", ""),
		FFmpeg:        str("KURGU_FFMPEG", "ffmpeg"),
		Encoder:       str("KURGU_RENDER_ENCODER", "libx264"),
		Chunks:        int(number("KURGU_RENDER_CHUNKS", 1, 1, 16)),
		Duty:          number("KURGU_RENDER_DUTY", 1, 0.05, 1),
		Threads:       int(number("KURGU_RENDER_THREADS", 0, 0, 64)),
		PreviewScale:  number("KURGU_RENDER_PREVIEW_SCALE", 0.5, 0.25, 1),
		FinalScale:    number("KURGU_RENDER_FINAL_SCALE", 1, 0.25, 2),
		GPU:           boolean("KURGU_RENDER_GPU", false),
	}
	cfg.Worker.CPULimit = int(number("KURGU_CPU_LIMIT", 0, 0, 100))
	cfg.Worker.Nice = int(number("KURGU_NICE", 0, 0, 19))
	cfg.Notify = Notify{
		SMTPHost: str("KURGU_SMTP_HOST", ""), SMTPUser: str("KURGU_SMTP_USERNAME", ""),
		SMTPPassword: str("KURGU_SMTP_PASSWORD", ""), SMTPFrom: str("KURGU_SMTP_FROM", ""),
		SMTPTLS: str("KURGU_SMTP_TLS", DefaultSMTPTLS), SMTPPort: int(number("KURGU_SMTP_PORT", 0, 0, 65535)),
		Every:         duration("KURGU_NOTIFY_EVERY", 10*time.Minute),
		OpenRouterKey: str("KURGU_OPENROUTER_API_KEY", ""), DecisionModel: str("KURGU_DECISION_MODEL", DefaultDecisionModel),
	}
	cfg.Remote = Remote{URL: strings.TrimRight(str("KURGU_REMOTE_URL", ""), "/"), Token: str("KURGU_WORKER_TOKEN", "")}
	if cfg.Remote.URL != "" && cfg.Remote.Token == "" {
		errs = append(errs, "KURGU_REMOTE_URL needs KURGU_WORKER_TOKEN")
	}
	if cfg.Remote.Token != "" && len(cfg.Remote.Token) < 24 {
		errs = append(errs, "KURGU_WORKER_TOKEN must be at least 24 characters")
	}
	cfg.Publishing = Publishing{
		YouTubeClientID: str("KURGU_YOUTUBE_CLIENT_ID", ""), YouTubeClientSecret: str("KURGU_YOUTUBE_CLIENT_SECRET", ""),
		SecretKeyFile: str("KURGU_SECRET_KEY_FILE", filepath.Join(dataDir, "secret.key")), SecretKey: str("KURGU_SECRET_KEY", ""),
	}
	if t := cfg.Notify.SMTPTLS; t != "starttls" && t != "tls" && t != "none" {
		errs = append(errs, "KURGU_SMTP_TLS: must be starttls, tls or none")
	}
	if e := cfg.Production.ClaudeEffort; e != "" && !slices.Contains([]string{"low", "medium", "high", "xhigh", "max"}, e) {
		errs = append(errs, "KURGU_CLAUDE_EFFORT: must be low, medium, high, xhigh or max")
	}
	if e := cfg.Production.Encoder; e != "libx264" && e != "h264_videotoolbox" {
		errs = append(errs, "KURGU_RENDER_ENCODER: must be libx264 or h264_videotoolbox")
	}
	concurrency, err := ParseConcurrency(str("KURGU_QUEUE_CONCURRENCY", DefaultConcurrency))
	if err != nil {
		errs = append(errs, "KURGU_QUEUE_CONCURRENCY: "+err.Error())
	}
	cfg.Worker.Concurrency = concurrency
	if raw := strings.TrimSpace(getenv("KURGU_HEAVY_QUEUES")); raw != "" {
		heavy, err := ParseQueues(raw)
		if err != nil {
			errs = append(errs, "KURGU_HEAVY_QUEUES: "+err.Error())
		}
		cfg.Worker.HeavyQueues = heavy
	}
	if cfg.SchedulerTick < time.Second {
		errs = append(errs, "KURGU_SCHEDULER_TICK: must be at least 1s")
	}
	if cfg.Worker.Lease < 3*time.Second {
		errs = append(errs, "KURGU_JOB_LEASE: must be at least 3s")
	}
	if u, err := url.Parse(cfg.PublicURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
		errs = append(errs, "KURGU_PUBLIC_URL: must be an http(s) origin such as https://kurgu.example.com")
	}
	if cfg.LogFormat != "text" && cfg.LogFormat != "json" {
		errs = append(errs, "KURGU_LOG_FORMAT: must be text or json")
	}
	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration:\n  %s", strings.Join(errs, "\n  "))
	}
	return cfg, nil
}

// ParseQueues parses "queue,queue"; "none" is no queue.
func ParseQueues(raw string) ([]jobdomain.Queue, error) {
	out := []jobdomain.Queue{}
	if strings.TrimSpace(raw) == "none" {
		return out, nil
	}
	for part := range strings.SplitSeq(raw, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		q, err := jobdomain.ParseQueue(part)
		if err != nil {
			return nil, fmt.Errorf("%q: unknown queue", part)
		}
		out = append(out, q)
	}
	return out, nil
}

// ParseConcurrency parses "queue=n,queue=n". Queues not listed get 0, so
// a host lists exactly the queues it serves.
func ParseConcurrency(raw string) (map[jobdomain.Queue]int, error) {
	out := map[jobdomain.Queue]int{}
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("%q: want queue=n", part)
		}
		q, err := jobdomain.ParseQueue(strings.TrimSpace(name))
		if err != nil {
			return nil, fmt.Errorf("%q: unknown queue", name)
		}
		if _, dup := out[q]; dup {
			return nil, fmt.Errorf("%q: listed twice", name)
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 0 || n > 16 {
			return nil, fmt.Errorf("%q: concurrency must be 0..16", part)
		}
		out[q] = n
	}
	return out, nil
}

// defaultPublicURL derives http://host:port from the listen address,
// using localhost for wildcard hosts.
func defaultPublicURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://localhost:8080"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func defaultOwner() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "kurgu"
	}
	return fmt.Sprintf("%s:%d", host, os.Getpid())
}
