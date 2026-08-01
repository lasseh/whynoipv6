//go:build integration

// The package-wide integration harness: server constructors, body decoders
// and the shared envelope shape. Feature fixtures (seedLeaderboard and
// friends) stay in their feature files.
package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/api"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

// newServer serves the router over the given pool — the one
// httptest.NewServer(api.NewRouter(...)) in the package.
func newServer(t *testing.T, pool *pgxpool.Pool, opts api.Options) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(api.NewRouter(pool, opts))
	t.Cleanup(srv.Close)
	return srv
}

// newAPI is the default harness: fresh DB + the leaderboard seed + default
// options.
func newAPI(t *testing.T) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()
	pool := pgtest.NewDB(t)
	seedLeaderboard(t, pool)
	return newServer(t, pool, api.Options{}), pool
}

func getJSON(t *testing.T, url string, out any) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return resp
}

func fetch(t *testing.T, url string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}

func decodeBody(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
}

type envelope struct {
	Items []map[string]json.RawMessage `json:"items"`
	Page  struct {
		NextCursor *string `json:"next_cursor"`
		PrevCursor *string `json:"prev_cursor"`
		HasMore    bool    `json:"has_more"`
	} `json:"page"`
	Meta struct {
		AsOf          string `json:"as_of"`
		Generation    int32  `json:"generation"`
		Count         *int64 `json:"count"`
		CountEstimate *int64 `json:"count_estimate"`
		License       string `json:"license"`
	} `json:"meta"`
}

func hosts(t *testing.T, items []map[string]json.RawMessage) []string {
	t.Helper()
	out := make([]string, len(items))
	for i, it := range items {
		if err := json.Unmarshal(it["host"], &out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}
