// Package notify implements the ops-webhook POST and the healthchecks.io
// ping client (09-ops.md §12). Delivery failures log at warn and are
// otherwise swallowed — a webhook outage must never stall a crawl. URLs are
// secrets and never appear in logs.
package notify

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// Client posts alerts and heartbeat pings. Empty URLs disable the
// respective channel (the dev/staging default).
type Client struct {
	WebhookURL     string        // ops.webhook_url
	HealthcheckURL string        // ops.healthcheck_url (this process's check)
	TickURL        string        // ops.healthcheck_tick_url (daily-tick check)
	MinInterval    time.Duration // ops.healthcheck_min_interval, 60s

	HTTP     *http.Client
	lastPing atomic.Int64 // unix nanos of the last success ping (throttle)
}

// New builds the client with a 10s HTTP timeout.
func New(webhookURL, healthcheckURL, tickURL string, minInterval time.Duration) *Client {
	return &Client{
		WebhookURL:     webhookURL,
		HealthcheckURL: healthcheckURL,
		TickURL:        tickURL,
		MinInterval:    minInterval,
		HTTP:           &http.Client{Timeout: 10 * time.Second},
	}
}

// Webhook posts a one-line text message to the ops webhook.
func (c *Client) Webhook(ctx context.Context, msg string) {
	if c.WebhookURL == "" {
		return
	}
	body := strings.NewReader(`{"text":` + jsonString(msg) + `}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.WebhookURL, body)
	if err != nil {
		slog.Warn("notify request build failed", "channel", "ops webhook", "err", err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	c.deliver(req, "ops webhook")
}

// HeartbeatOK pings this process's healthchecks.io success URL, throttled
// to one ping per MinInterval.
func (c *Client) HeartbeatOK(ctx context.Context) {
	if c.HealthcheckURL == "" {
		return
	}
	now := time.Now().UnixNano()
	last := c.lastPing.Load()
	if last != 0 && time.Duration(now-last) < c.MinInterval {
		return
	}
	if !c.lastPing.CompareAndSwap(last, now) {
		return // another goroutine pinged concurrently
	}
	c.ping(ctx, c.HealthcheckURL, "heartbeat")
}

// HeartbeatFail pings the /fail endpoint of this process's check (preflight
// failure); never throttled.
func (c *Client) HeartbeatFail(ctx context.Context) {
	if c.HealthcheckURL == "" {
		return
	}
	c.ping(ctx, strings.TrimSuffix(c.HealthcheckURL, "/")+"/fail", "heartbeat fail")
}

// PingTick pings the daily-tick check (step 7, only on core success).
func (c *Client) PingTick(ctx context.Context) {
	if c.TickURL == "" {
		return
	}
	c.ping(ctx, c.TickURL, "tick heartbeat")
}

func (c *Client) ping(ctx context.Context, url, what string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		slog.Warn("notify request build failed", "channel", what, "err", err.Error())
		return
	}
	c.deliver(req, what)
}

// deliver executes the request, logging failures at warn with the URL
// redacted (URLs are secrets — 09-ops.md §1).
func (c *Client) deliver(req *http.Request, what string) {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		slog.Warn("notify delivery failed", "channel", what, "err", redactURLs(err.Error(), req.URL.String()))
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		slog.Warn("notify delivery failed", "channel", what, "status", resp.StatusCode)
	}
}

// redactURLs strips the secret URL from an error string.
func redactURLs(msg, url string) string {
	return strings.ReplaceAll(msg, url, "<redacted>")
}

// jsonString minimally JSON-encodes a string (no deps, no error path).
func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(` `)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
