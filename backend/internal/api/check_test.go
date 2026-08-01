package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/observe"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// fakeCheckStore is the in-memory adapter at the checkStore seam: the whole
// §5.1.1 processing order — validation → rate limit → re-entry → domain
// dedupe → job dedupe → locked enqueue — runs against it without a database.
type fakeCheckStore struct {
	ipN, globalN             int32 // fast-path window counts
	lockedIPN, lockedGlobalN int32 // counts as seen under the advisory lock

	confirmed    map[string]db.DomainConfirmedRow
	confirmedErr error
	dedupe       map[string]db.CheckJobDedupeRow
	jobs         map[int64]db.CheckJobByIDRow
	detail       []byte

	reentries []string
	enqueued  []string
}

func (f *fakeCheckStore) RatePrefix(context.Context, netip.Prefix) (db.CheckJobRatePrefixRow, error) {
	return db.CheckJobRatePrefixRow{N: f.ipN}, nil
}

func (f *fakeCheckStore) RateGlobal(context.Context) (db.CheckJobRateGlobalRow, error) {
	return db.CheckJobRateGlobalRow{N: f.globalN}, nil
}

func (f *fakeCheckStore) Reentry(_ context.Context, host string) error {
	f.reentries = append(f.reentries, host)
	return nil
}

func (f *fakeCheckStore) Confirmed(_ context.Context, host string) (db.DomainConfirmedRow, error) {
	if f.confirmedErr != nil {
		return db.DomainConfirmedRow{}, f.confirmedErr
	}
	if row, ok := f.confirmed[host]; ok {
		return row, nil
	}
	return db.DomainConfirmedRow{}, pgx.ErrNoRows
}

func (f *fakeCheckStore) JobDedupe(_ context.Context, host string, _ time.Duration) (db.CheckJobDedupeRow, error) {
	if row, ok := f.dedupe[host]; ok {
		return row, nil
	}
	return db.CheckJobDedupeRow{}, pgx.ErrNoRows
}

func (f *fakeCheckStore) JobByID(_ context.Context, id int64) (db.CheckJobByIDRow, error) {
	if job, ok := f.jobs[id]; ok {
		return job, nil
	}
	return db.CheckJobByIDRow{}, pgx.ErrNoRows
}

func (f *fakeCheckStore) EnqueueLocked(_ context.Context, host string, _ netip.Addr, _ netip.Prefix,
	ipCap, globalCap int,
) (enqueueResult, error) {
	if int(f.lockedIPN) >= ipCap {
		return enqueueResult{OverIP: true, IPWindow: db.CheckJobRatePrefixRow{N: f.lockedIPN}}, nil
	}
	if int(f.lockedGlobalN) >= globalCap {
		return enqueueResult{OverGlobal: true, IPWindow: db.CheckJobRatePrefixRow{N: f.lockedIPN},
			GlobalWindow: db.CheckJobRateGlobalRow{N: f.lockedGlobalN}}, nil
	}
	f.enqueued = append(f.enqueued, host)
	return enqueueResult{ID: int64(len(f.enqueued)), CreatedAt: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		IPWindow: db.CheckJobRatePrefixRow{N: f.lockedIPN}}, nil
}

func (f *fakeCheckStore) LatestScanDetail(context.Context, int64) ([]byte, error) {
	if f.detail == nil {
		return nil, pgx.ErrNoRows
	}
	return f.detail, nil
}

func (f *fakeCheckStore) LiveLinks(context.Context, checker.ScanResult, bool) []observe.LinkedResource {
	return nil
}

func checkServer(f *fakeCheckStore) *Server {
	return &Server{
		opts: Options{RateIPPerHour: 10, RateGlobalPerHour: 500,
			DedupeWindow: time.Hour, LinkTTL: 168 * time.Hour},
		checks: f,
	}
}

func postCheckReq(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/check", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func pgTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func TestPostCheckValidation(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
		detail     string
	}{
		{"not JSON", "not json", http.StatusUnsupportedMediaType, ""},
		{"non-string host", `{"host":123}`, http.StatusBadRequest, "host must be a string"},
		{"missing host key", `{}`, http.StatusBadRequest, "the request body needs a host key"},
		{"invalid host", `{"host":"not a domain!"}`, http.StatusBadRequest, "not a valid public domain name"},
		{"unknown TLD", `{"host":"foo.nosuchtldzzz"}`, http.StatusBadRequest, "the host is not under a known public-suffix TLD"},
		{"oversize body", `{"host":"` + strings.Repeat("a", 5000) + `"}`, http.StatusBadRequest, "request body too large"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeCheckStore{}
			rec := httptest.NewRecorder()
			checkServer(f).postCheck(rec, postCheckReq(tc.body))
			if rec.Code != tc.status {
				t.Fatalf("code = %d, want %d (%s)", rec.Code, tc.status, rec.Body.String())
			}
			if tc.detail != "" && !strings.Contains(rec.Body.String(), tc.detail) {
				t.Errorf("body = %s", rec.Body.String())
			}
			if len(f.reentries) != 0 || len(f.enqueued) != 0 {
				t.Error("store touched on a validation failure")
			}
		})
	}
}

func TestPostCheckRateLimits(t *testing.T) {
	t.Run("prefix window rejects before re-entry", func(t *testing.T) {
		f := &fakeCheckStore{ipN: 10}
		rec := httptest.NewRecorder()
		checkServer(f).postCheck(rec, postCheckReq(`{"host":"example.com"}`))
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("code = %d", rec.Code)
		}
		if rec.Header().Get("Retry-After") != "3600" { // no min_created → full window
			t.Errorf("Retry-After = %q", rec.Header().Get("Retry-After"))
		}
		if got := rec.Header().Get("RateLimit"); !strings.Contains(got, "remaining=0") {
			t.Errorf("RateLimit = %q", got)
		}
		if len(f.reentries) != 0 {
			t.Error("re-entry ran on a rate-limited request")
		}
	})

	t.Run("global window rejects", func(t *testing.T) {
		f := &fakeCheckStore{globalN: 500}
		rec := httptest.NewRecorder()
		checkServer(f).postCheck(rec, postCheckReq(`{"host":"example.com"}`))
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("code = %d", rec.Code)
		}
	})

	t.Run("locked re-check rejects the race", func(t *testing.T) {
		// The fast-path reads pass; the count as seen under the advisory
		// lock trips the cap — previously only reachable with a real race.
		f := &fakeCheckStore{ipN: 0, lockedIPN: 10}
		rec := httptest.NewRecorder()
		checkServer(f).postCheck(rec, postCheckReq(`{"host":"example.com"}`))
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("code = %d", rec.Code)
		}
		if len(f.enqueued) != 0 {
			t.Error("job inserted past the locked cap")
		}
	})
}

func TestPostCheckEnqueue(t *testing.T) {
	f := &fakeCheckStore{ipN: 2, lockedIPN: 3}
	rec := httptest.NewRecorder()
	checkServer(f).postCheck(rec, postCheckReq(`{"host":"example.com"}`))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("code = %d (%s)", rec.Code, rec.Body.String())
	}
	if !slices.Contains(f.reentries, "example.com") {
		t.Error("lifecycle re-entry did not run")
	}
	if !slices.Contains(f.enqueued, "example.com") {
		t.Error("job not enqueued")
	}
	if loc := rec.Header().Get("Location"); loc != "/check/1" {
		t.Errorf("Location = %q", loc)
	}
	// Headers reflect the LOCKED count (3), not the stale fast-path read (2).
	if got := rec.Header().Get("RateLimit"); !strings.Contains(got, "remaining=6") {
		t.Errorf("RateLimit = %q", got)
	}
	var body struct {
		ID     int64  `json:"id"`
		Host   string `json:"host"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil ||
		body.ID != 1 || body.Host != "example.com" || body.Status != "pending" {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestPostCheckJobDedupe(t *testing.T) {
	created := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	f := &fakeCheckStore{dedupe: map[string]db.CheckJobDedupeRow{
		"example.com": {ID: 7, Host: "example.com", Status: db.CheckJobStatus("done"),
			Result: []byte(`{"ok":true}`), CreatedAt: pgTime(created), CompletedAt: pgTime(created)},
	}}
	rec := httptest.NewRecorder()
	checkServer(f).postCheck(rec, postCheckReq(`{"host":"example.com"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d (%s)", rec.Code, rec.Body.String())
	}
	var env CheckEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.ID == nil || *env.ID != 7 || !env.Cached || env.Status != "done" {
		t.Errorf("envelope = %s", rec.Body.String())
	}
	if len(f.enqueued) != 0 {
		t.Error("dedupe hit still enqueued a job")
	}
	if !slices.Contains(f.reentries, "example.com") {
		t.Error("re-entry must run on dedupe hits too (§5.1.6)")
	}
}

func TestPostCheckDomainDedupe(t *testing.T) {
	sr := checker.ScanResult{Domain: "example.com", Results: map[string]checker.Result{},
		ScannedAt: time.Now().UTC()}
	detail, err := json.Marshal(sr)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeCheckStore{
		confirmed: map[string]db.DomainConfirmedRow{
			"example.com": {ID: 1, Kind: db.DomainKind("apex"),
				Classification: db.Classification("unknown"),
				LastCheckedAt:  pgTime(time.Now().Add(-10 * time.Minute))},
		},
		detail: detail,
	}
	rec := httptest.NewRecorder()
	checkServer(f).postCheck(rec, postCheckReq(`{"host":"example.com"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d (%s)", rec.Code, rec.Body.String())
	}
	var env CheckEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	// The synthetic done envelope: null id, cached, evidence present.
	if env.ID != nil || !env.Cached || env.Status != "done" || env.Result == nil {
		t.Errorf("envelope = %s", rec.Body.String())
	}
	if len(f.enqueued) != 0 {
		t.Error("fresh crawl still enqueued a job")
	}
}

func TestGetCheck(t *testing.T) {
	done := db.CheckJobByIDRow{ID: 3, Host: "example.com", Status: db.CheckJobStatus("done"),
		CreatedAt: pgTime(time.Now()), CompletedAt: pgTime(time.Now())}
	pending := db.CheckJobByIDRow{ID: 4, Host: "example.com", Status: db.CheckJobStatus("pending"),
		CreatedAt: pgTime(time.Now())}
	f := &fakeCheckStore{jobs: map[int64]db.CheckJobByIDRow{3: done, 4: pending}}
	srv := checkServer(f)
	router := testCheckRouter(srv)

	t.Run("non-numeric id is 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/check/abc", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d", rec.Code)
		}
	})
	t.Run("missing job is 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/check/99", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d", rec.Code)
		}
	})
	t.Run("terminal job caches", func(t *testing.T) {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/check/3", nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "public, max-age=60" {
			t.Fatalf("code = %d cache = %q", rec.Code, rec.Header().Get("Cache-Control"))
		}
	})
	t.Run("in-flight job stays no-store", func(t *testing.T) {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/check/4", nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("code = %d cache = %q", rec.Code, rec.Header().Get("Cache-Control"))
		}
	})
}

func TestGetLatestCheck(t *testing.T) {
	t.Run("job source inside the TTL", func(t *testing.T) {
		f := &fakeCheckStore{dedupe: map[string]db.CheckJobDedupeRow{
			"example.com": {ID: 5, Host: "example.com", Status: db.CheckJobStatus("done"),
				CreatedAt: pgTime(time.Now()), CompletedAt: pgTime(time.Now())},
		}}
		rec := httptest.NewRecorder()
		checkServer(f).getLatestCheck(rec,
			httptest.NewRequest(http.MethodGet, "/check/latest?host=example.com", nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "public, max-age=60" {
			t.Fatalf("code = %d cache = %q", rec.Code, rec.Header().Get("Cache-Control"))
		}
		var env CheckEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || !env.Cached {
			t.Errorf("envelope = %s", rec.Body.String())
		}
	})
	t.Run("miss is 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		checkServer(&fakeCheckStore{}).getLatestCheck(rec,
			httptest.NewRequest(http.MethodGet, "/check/latest?host=example.com", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d", rec.Code)
		}
	})
	t.Run("invalid host is 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		checkServer(&fakeCheckStore{}).getLatestCheck(rec,
			httptest.NewRequest(http.MethodGet, "/check/latest?host=%21bad", nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d", rec.Code)
		}
	})
}

// testCheckRouter mounts only the check routes so chi URL params resolve.
func testCheckRouter(s *Server) http.Handler {
	r := chi.NewRouter()
	r.Get("/check/latest", s.getLatestCheck)
	r.Get("/check/{id}", s.getCheck)
	return r
}
