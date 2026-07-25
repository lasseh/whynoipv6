package ingest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	trancoListIDURL = "https://tranco-list.eu/top-1m-id"
	trancoListURL   = "https://tranco-list.eu/top-1m.csv.zip"
)

// HTTPTrancoSource is the production TrancoSource: 60 s total timeout per
// request, no retries inside an attempt (06-ingest.md §2.2).
type HTTPTrancoSource struct {
	Client *http.Client
}

// NewHTTPTrancoSource builds the production source with the 60 s per-request
// timeout (06-ingest.md §2.2).
func NewHTTPTrancoSource() *HTTPTrancoSource {
	return &HTTPTrancoSource{Client: &http.Client{Timeout: 60 * time.Second}}
}

// ListID fetches the current list ID and rejects a malformed one — non-empty,
// ≤16 chars, alphanumeric (06-ingest.md §2.2 step 2).
func (s *HTTPTrancoSource) ListID(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trancoListIDURL, http.NoBody)
	if err != nil {
		return "", err
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("list-id endpoint: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(body))
	if id == "" || len(id) > 16 || !isAlnum(id) {
		return "", fmt.Errorf("list-id endpoint returned malformed id %q", id)
	}
	return id, nil
}

// List downloads the zip artifact with a conditional GET on etag; a 304 comes
// back as an archive marked NotModified (06-ingest.md §2.2 step 3).
func (s *HTTPTrancoSource) List(ctx context.Context, etag string) (*TrancoArchive, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trancoListURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotModified {
		return &TrancoArchive{NotModified: true}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list download: %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	arch := &TrancoArchive{Zip: data, ETag: resp.Header.Get("ETag")}
	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		if t, err := http.ParseTime(lm); err == nil {
			arch.LastModified = t
		}
	}
	return arch, nil
}

func isAlnum(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}
