package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestNotify (P2.13): the client posts to a stub webhook and pings stub
// healthchecks URLs; URLs are redacted in logs; empty URLs disable channels.
func TestNotify(t *testing.T) {
	var mu sync.Mutex
	var webhookBodies []string
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPost {
			b, _ := io.ReadAll(r.Body)
			webhookBodies = append(webhookBodies, string(b))
		}
		paths = append(paths, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL+"/hook/secret-token", srv.URL+"/ping/uuid-1", srv.URL+"/ping/tick", 60*time.Second)
	ctx := context.Background()

	c.Webhook(ctx, `tranco import aborted: "note"`)
	c.HeartbeatOK(ctx)
	c.HeartbeatOK(ctx) // throttled: within MinInterval, no second ping
	c.HeartbeatFail(ctx)
	c.PingTick(ctx)

	mu.Lock()
	defer mu.Unlock()
	if len(webhookBodies) != 1 || !strings.Contains(webhookBodies[0], `tranco import aborted: \"note\"`) {
		t.Errorf("webhook bodies = %q", webhookBodies)
	}
	want := map[string]int{"/hook/secret-token": 1, "/ping/uuid-1": 1, "/ping/uuid-1/fail": 1, "/ping/tick": 1}
	got := map[string]int{}
	for _, p := range paths {
		got[p]++
	}
	for p, n := range want {
		if got[p] != n {
			t.Errorf("path %s hit %d times, want %d (all: %v)", p, got[p], n, paths)
		}
	}
	if len(paths) != 4 {
		t.Errorf("total requests = %d, want 4 (throttle must swallow the 2nd OK ping)", len(paths))
	}
}

// TestNotifyRedaction: a delivery failure must not leak the URL into logs.
func TestNotifyRedaction(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(old)

	// Unroutable target → transport error containing the URL.
	c := New("http://127.0.0.1:1/hook/supersecret", "", "", time.Minute)
	c.HTTP.Timeout = 200 * time.Millisecond
	c.Webhook(context.Background(), "boom")

	if strings.Contains(buf.String(), "supersecret") {
		t.Errorf("log leaked the webhook URL: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "delivery failed") {
		t.Errorf("delivery failure not logged: %s", buf.String())
	}

	// A URL the request builder rejects comes back inside *url.Error with
	// the URL quoted — the build-failure log must not echo it either.
	buf.Reset()
	c = New("http://127.0.0.1:1/hook/supersecret\n", "", "", time.Minute)
	c.Webhook(context.Background(), "boom")
	if strings.Contains(buf.String(), "supersecret") {
		t.Errorf("log leaked the webhook URL on build failure: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "request build failed") {
		t.Errorf("build failure not logged: %s", buf.String())
	}
}

// TestHeartbeatFailKeepsQuery: /fail goes on the path, ahead of the query
// string a slug-style check URL carries.
func TestHeartbeatFailKeepsQuery(t *testing.T) {
	var mu sync.Mutex
	var uris []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		uris = append(uris, r.URL.RequestURI())
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New("", srv.URL+"/ping/crawler?create=1", "", time.Minute)
	c.HeartbeatFail(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if len(uris) != 1 || uris[0] != "/ping/crawler/fail?create=1" {
		t.Errorf("fail ping hit %q, want [/ping/crawler/fail?create=1]", uris)
	}
}

// TestNotifyDisabled: empty URLs are no-ops.
func TestNotifyDisabled(t *testing.T) {
	c := New("", "", "", time.Minute)
	ctx := context.Background()
	c.Webhook(ctx, "x")
	c.HeartbeatOK(ctx)
	c.HeartbeatFail(ctx)
	c.PingTick(ctx) // must not panic or dial anything
}

// TestWebhookBodyContract pins the receiver contract 09-ops §12 records
// (review issue 40): every producer posting to ops.webhook_url sends a
// human-readable "text" member, because a Slack incoming webhook rejects a
// body without one as 400 invalid_payload. The systemd notify unit sends
// the same key; its body lives in deploy/systemd/whynoipv6-notify@.service.
func TestWebhookBodyContract(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	New(srv.URL, "", "", time.Minute).Webhook(context.Background(), "crawler preflight failed")

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, body)
	}
	text, ok := got["text"].(string)
	if !ok {
		t.Fatalf("body = %s, want a string \"text\" member", body)
	}
	if text != "crawler preflight failed" {
		t.Errorf("text = %q, want the message verbatim", text)
	}
}
