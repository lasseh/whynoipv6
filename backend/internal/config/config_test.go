package config

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:pw@dbhost:5432/whynoipv6")

	cfg, err := Load("crawler")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Int("worker_slots"); got != 64 {
		t.Errorf("worker_slots default = %d, want 64", got)
	}
	if got := cfg.String("claim.order"); got != "rank" {
		t.Errorf("claim.order default = %q, want rank", got)
	}
	if got := cfg.StringSlice("resolver.bulk_upstreams"); len(got) != 2 || got[0] != "127.0.0.1:53" {
		t.Errorf("resolver.bulk_upstreams default = %v", got)
	}
	if !cfg.Bool("crawler.resources.enabled") {
		t.Error("crawler.resources.enabled default = false, want true (resources always on)")
	}
	if got := cfg.APIListen; got != "[::1]:8080" {
		t.Errorf("API_LISTEN default = %q, want [::1]:8080", got)
	}
}

func TestConfigRequiredDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load("api"); err == nil {
		t.Fatal("Load with empty DATABASE_URL: want error, got nil")
	}
}

// TestInstallLoggerShipsToTaillight exercises the full fan-out path: a record
// logged through the installed logger reaches the Taillight ingest endpoint
// with the right service/component/auth, and flush drains the batch.
func TestInstallLoggerShipsToTaillight(t *testing.T) {
	var (
		mu   sync.Mutex
		body []byte
		auth string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = append(body, b...)
		auth = r.Header.Get("Authorization")
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"accepted":1}`))
	}))
	defer srv.Close()

	t.Setenv("DATABASE_URL", "postgres://u:pw@dbhost:5432/whynoipv6")
	t.Setenv("TAILLIGHT_URL", srv.URL+"/api/v1/applog/ingest")
	t.Setenv("TAILLIGHT_API_KEY", "test-key")

	cfg, err := Load("crawler")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	log, flush, err := cfg.InstallLogger()
	if err != nil {
		t.Fatalf("InstallLogger: %v", err)
	}
	log.Info("taillight integration probe", "domain", "example.com")
	flush()

	mu.Lock()
	out, gotAuth := string(body), auth
	mu.Unlock()
	for _, want := range []string{
		`"logs":[`,
		`"service":"whynoipv6"`,
		`"component":"crawler"`,
		`"level":"INFO"`,
		`"msg":"taillight integration probe"`,
		`"domain":"example.com"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("shipped payload missing %s: %s", want, out)
		}
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", gotAuth)
	}
}

// TestInstallLoggerTaillightLogLevel: taillight.log_level filters the shipper
// only. With LOG_LEVEL=debug the local stdout handler stays at debug (journald
// sees per-domain lines) while the shipper, at info, drops them.
func TestInstallLoggerTaillightLogLevel(t *testing.T) {
	var (
		mu   sync.Mutex
		body []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = append(body, b...)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"accepted":1}`))
	}))
	defer srv.Close()

	t.Setenv("DATABASE_URL", "postgres://u:pw@dbhost:5432/whynoipv6")
	t.Setenv("TAILLIGHT_URL", srv.URL+"/api/v1/applog/ingest")
	t.Setenv("TAILLIGHT_API_KEY", "test-key")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("TAILLIGHT_LOG_LEVEL", "info")

	cfg, err := Load("crawler")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("cfg.LogLevel = %v, want debug (the local handler must stay verbose)", cfg.LogLevel)
	}
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	log, flush, err := cfg.InstallLogger()
	if err != nil {
		t.Fatalf("InstallLogger: %v", err)
	}
	if !log.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("logger is not debug-enabled: the local handler was filtered to the shipper's level")
	}
	log.Debug("per-domain debug line", "domain", "example.com")
	log.Info("lifecycle line")
	flush()

	mu.Lock()
	out := string(body)
	mu.Unlock()
	// Asserting both in one payload: a bare absence check would also pass if
	// nothing shipped at all.
	if !strings.Contains(out, `"msg":"lifecycle line"`) {
		t.Errorf("info record was not shipped: %s", out)
	}
	if strings.Contains(out, "per-domain debug line") {
		t.Errorf("debug record shipped despite TAILLIGHT_LOG_LEVEL=info: %s", out)
	}
}

// TestInstallLoggerBadTaillightLogLevel: an unknown shipper level is fatal at
// startup, like a malformed taillight.url.
func TestInstallLoggerBadTaillightLogLevel(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:pw@dbhost:5432/whynoipv6")
	t.Setenv("TAILLIGHT_URL", "https://taillight.example.com/api/v1/applog/ingest")
	t.Setenv("TAILLIGHT_LOG_LEVEL", "verbose")

	cfg, err := Load("api")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	if _, _, err := cfg.InstallLogger(); err == nil {
		t.Fatal("InstallLogger with unknown taillight.log_level: want error, got nil")
	}
}

// TestInstallLoggerBadTaillightURL: a malformed endpoint is a fatal startup
// error, consistent with every other misconfiguration.
func TestInstallLoggerBadTaillightURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:pw@dbhost:5432/whynoipv6")
	t.Setenv("TAILLIGHT_URL", "not-a-url")

	cfg, err := Load("api")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	if _, _, err := cfg.InstallLogger(); err == nil {
		t.Fatal("InstallLogger with malformed taillight.url: want error, got nil")
	}
}

func TestConfigRedaction(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://whynoipv6:s3cretpw@dbhost:5432/whynoipv6?sslmode=disable")
	t.Setenv("OPS_WEBHOOK_URL", "https://hooks.example.com/T000/B000/supersecrettoken")

	cfg, err := Load("crawler")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	cfg.LogSummary(log, slog.LevelInfo)
	out := buf.String()

	for _, leak := range []string{"s3cretpw", "supersecrettoken", "hooks.example.com"} {
		if strings.Contains(out, leak) {
			t.Errorf("startup summary leaks secret %q", leak)
		}
	}
	if !strings.Contains(out, "postgres://whynoipv6@dbhost:5432/whynoipv6") {
		t.Errorf("summary missing redacted host+db DSN: %s", out)
	}
	if !strings.Contains(out, `"ops.webhook_url":"set"`) {
		t.Errorf("summary should log webhook as set: %s", out)
	}
	if !strings.Contains(out, `"ops.healthcheck_url":"unset"`) {
		t.Errorf("summary should log unset ping URL as unset: %s", out)
	}
}

// TestRedactDSN: the libpq keyword form is a valid pgx DSN that url.Parse
// accepts whole as a path — it must not be echoed with its password.
func TestRedactDSN(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"url", "postgres://whynoipv6:s3cretpw@dbhost:5432/whynoipv6?sslmode=disable", "postgres://whynoipv6@dbhost:5432/whynoipv6"},
		{"keyword form", "host=dbhost user=whynoipv6 password=s3cretpw dbname=whynoipv6", "set"},
		{"bare host", "dbhost", "set"},
		{"unparseable", "postgres://u:p%zz@dbhost/db", "invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactDSN(tc.in); got != tc.want {
				t.Errorf("redactDSN(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
