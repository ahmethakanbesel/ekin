// Package config loads process configuration from KURGU_* environment
// variables. Every setting has a default that works for local
// development on a laptop.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	jobdomain "kurgu/internal/domain/job/v1"
)

// Config is the process configuration.
type Config struct {
	HTTPAddr       string
	DataDir        string
	DBPath         string
	MigrateOnStart bool
	SchedulerTick  time.Duration
	LogFormat      string // text | json
	LogLevel       string // debug | info | warn | error
	Worker         Worker
}

// Worker configures the job runner (PLAN.md §4).
type Worker struct {
	Owner          string
	Concurrency    map[jobdomain.Queue]int
	HeavyExclusive bool
	Cooldown       time.Duration
	Paused         bool
	Lease          time.Duration
}

// DefaultConcurrency suits the 2 vCPU VPS: one of everything, two for
// network-bound io.
const DefaultConcurrency = "control=1,agent=1,tts=1,render=1,extract=1,io=2"

// Load reads the environment.
func Load() (Config, error) {
	return load(os.Getenv)
}

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
	cfg := Config{
		HTTPAddr:       str("KURGU_HTTP_ADDR", "127.0.0.1:8080"),
		DataDir:        dataDir,
		DBPath:         str("KURGU_DB_PATH", filepath.Join(dataDir, "kurgu.db")),
		MigrateOnStart: boolean("KURGU_MIGRATE_ON_START", true),
		SchedulerTick:  duration("KURGU_SCHEDULER_TICK", 30*time.Second),
		LogFormat:      str("KURGU_LOG_FORMAT", "text"),
		LogLevel:       str("KURGU_LOG_LEVEL", "info"),
		Worker: Worker{
			Owner:          str("KURGU_WORKER_OWNER", defaultOwner()),
			HeavyExclusive: boolean("KURGU_HEAVY_EXCLUSIVE", true),
			Cooldown:       duration("KURGU_HEAVY_COOLDOWN", 30*time.Second),
			Paused:         boolean("KURGU_WORKER_PAUSED", false),
			Lease:          duration("KURGU_JOB_LEASE", 2*time.Minute),
		},
	}
	concurrency, err := ParseConcurrency(str("KURGU_QUEUE_CONCURRENCY", DefaultConcurrency))
	if err != nil {
		errs = append(errs, "KURGU_QUEUE_CONCURRENCY: "+err.Error())
	}
	cfg.Worker.Concurrency = concurrency
	if cfg.SchedulerTick < time.Second {
		errs = append(errs, "KURGU_SCHEDULER_TICK: must be at least 1s")
	}
	if cfg.Worker.Lease < 3*time.Second {
		errs = append(errs, "KURGU_JOB_LEASE: must be at least 3s")
	}
	if cfg.LogFormat != "text" && cfg.LogFormat != "json" {
		errs = append(errs, "KURGU_LOG_FORMAT: must be text or json")
	}
	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration:\n  %s", strings.Join(errs, "\n  "))
	}
	return cfg, nil
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

func defaultOwner() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "kurgu"
	}
	return fmt.Sprintf("%s:%d", host, os.Getpid())
}
