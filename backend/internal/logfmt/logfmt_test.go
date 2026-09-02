package logfmt

import (
	"bytes"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

var updateGoldens = flag.Bool("update", false, "rewrite golden files")

// render is the table-test shorthand: one raw line through one Formatter.
func render(t *testing.T, opts Options, raw string) (string, bool) {
	t.Helper()
	out, ok := New(opts).Append(nil, []byte(raw))
	return string(out), ok
}

func TestAppend(t *testing.T) {
	const stamp = `"time":"2026-08-22T03:00:33.221456789+02:00"`
	cases := []struct {
		name string
		opts Options
		raw  string
		want []string // substrings that must be present
		skip []string // substrings that must be absent
		drop bool     // Append reports nothing rendered
	}{
		{
			name: "record renders stamp, level and attributes",
			raw:  `{` + stamp + `,"level":"INFO","msg":"crawler started","worker_slots":64}`,
			want: []string{"03:00:33.221", "INFO ", "crawler started", "worker_slots=64"},
		},
		{
			name: "run_id shortens to eight characters",
			raw:  `{` + stamp + `,"level":"INFO","msg":"m","run_id":"7c9e6679-7425-40de-944b-e07fc1f90ae7"}`,
			want: []string{"run_id=7c9e6679"},
			skip: []string{"7c9e6679-7425"},
		},
		{
			name: "a run_id shorter than the cut survives whole",
			raw:  `{` + stamp + `,"level":"INFO","msg":"m","run_id":"abc"}`,
			want: []string{"run_id=abc"},
		},
		{
			name: "full keeps the raw stamp and the whole run_id",
			opts: Options{Full: true},
			raw:  `{` + stamp + `,"level":"INFO","msg":"m","run_id":"7c9e6679-7425-40de-944b-e07fc1f90ae7"}`,
			want: []string{"2026-08-22T03:00:33.221456789+02:00", "run_id=7c9e6679-7425-40de-944b-e07fc1f90ae7"},
		},
		{
			name: "duplicate keys are both kept",
			raw:  `{` + stamp + `,"level":"INFO","msg":"m","domain":"a.no","domain":"b.no"}`,
			want: []string{"domain=a.no", "domain=b.no"},
		},
		{
			name: "attribute order follows the record",
			raw:  `{` + stamp + `,"level":"INFO","msg":"m","z":1,"a":2}`,
			want: []string{"z=1 a=2"},
		},
		{
			name: "hidden keys are dropped",
			opts: Options{Hide: []string{"component", "worker"}},
			raw:  `{` + stamp + `,"level":"INFO","msg":"m","component":"crawler","worker":"v6:1","domain":"a.no"}`,
			want: []string{"domain=a.no"},
			skip: []string{"component=", "worker="},
		},
		{
			name: "strings are unquoted and control characters escaped",
			raw:  `{` + stamp + `,"level":"ERROR","msg":"m","err":"constraint \"pkey\"\n  (SQLSTATE 23505)"}`,
			want: []string{`err=constraint "pkey"\n  (SQLSTATE 23505)`},
		},
		{
			name: "a passthrough line is escaped, C1 included",
			raw:  "panic: \x1b[2Jboom \u009b31mred",
			want: []string{`panic: \x1b[2Jboom \x9b31mred`},
		},
		{
			name: "non-string values render as JSON",
			raw:  `{` + stamp + `,"level":"INFO","msg":"m","up":["1.1.1.1","9.9.9.9"],"n":null,"ok":true}`,
			want: []string{`up=["1.1.1.1","9.9.9.9"]`, "n=null", "ok=true"},
		},
		{
			name: "a wide record collapses to a count",
			raw:  wideRecord(71),
			want: []string{"k00=0", "k01=1", "k02=2", "… +68 more (--full)"},
			skip: []string{"k20="},
		},
		{
			name: "full expands a wide record",
			opts: Options{Full: true},
			raw:  wideRecord(71),
			want: []string{"k70=70"},
			skip: []string{"more (--full)"},
		},
		{
			name: "a level below the floor is skipped",
			opts: Options{MinLevel: slog.LevelWarn},
			raw:  `{` + stamp + `,"level":"DEBUG","msg":"m"}`,
			drop: true,
		},
		{
			name: "a level at the floor is kept",
			opts: Options{MinLevel: slog.LevelWarn},
			raw:  `{` + stamp + `,"level":"ERROR","msg":"m"}`,
			want: []string{"ERROR"},
		},
		{
			name: "the offset levels slog emits parse and pass the filter",
			opts: Options{MinLevel: slog.LevelWarn},
			raw:  `{` + stamp + `,"level":"ERROR+4","msg":"m"}`,
			want: []string{"ERROR+4"},
		},
		{
			name: "the zero Options keep debug records",
			raw:  `{` + stamp + `,"level":"DEBUG","msg":"m"}`,
			want: []string{"DEBUG"},
		},
		{
			name: "a non-JSON line passes through untouched",
			raw:  "panic: runtime error: invalid memory address",
			want: []string{"panic: runtime error: invalid memory address"},
		},
		{
			name: "a non-JSON line is not level-filtered away",
			opts: Options{MinLevel: slog.LevelError},
			raw:  "Started WhyNoIPv6 crawler.",
			want: []string{"Started WhyNoIPv6 crawler."},
		},
		{
			name: "a blank line renders nothing",
			raw:  "   ",
			drop: true,
		},
		{
			name: "a record missing time, level and msg still renders",
			raw:  `{"domain":"a.no"}`,
			want: []string{"domain=a.no"},
		},
		{
			name: "an unparseable time pads the stamp column",
			raw:  `{"time":"not-a-time","level":"INFO","msg":"m","domain":"a.no"}`,
			want: []string{strings.Repeat(" ", stampWidth) + " INFO "},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := render(t, tc.opts, tc.raw)
			if ok == tc.drop {
				t.Fatalf("Append reported %v, want %v: %q", ok, !tc.drop, got)
			}
			if tc.drop {
				return
			}
			if !strings.HasSuffix(got, "\n") {
				t.Errorf("rendered line does not end in a newline: %q", got)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in:\n%s", want, got)
				}
			}
			for _, skip := range tc.skip {
				if strings.Contains(got, skip) {
					t.Errorf("unexpected %q in:\n%s", skip, got)
				}
			}
		})
	}
}

// TestAppendKeepsLongMessages: the message column is a minimum, not a cut —
// truncating a message to hold a column loses the only text a reader scans.
func TestAppendKeepsLongMessages(t *testing.T) {
	const msg = "shutdown: draining in-flight scans before the budget expires"
	got, _ := render(t, Options{MsgWidth: 10}, `{"level":"INFO","msg":"`+msg+`","n":1}`)
	if !strings.Contains(got, msg) {
		t.Errorf("message was truncated:\n%s", got)
	}
	if !strings.Contains(got, msg+" n=1") {
		t.Errorf("attributes should follow the whole message:\n%s", got)
	}
}

// TestAppendOversizedToken: a value wider than the whole wrap column must be
// emitted alone and overflow, never split and never looped over.
func TestAppendOversizedToken(t *testing.T) {
	long := strings.Repeat("x", 400)
	got, ok := render(t, Options{Width: 80}, `{"level":"ERROR","msg":"m","err":"`+long+`"}`)
	if !ok {
		t.Fatal("Append dropped the record")
	}
	if !strings.Contains(got, "err="+long) {
		t.Error("the oversized value was split or dropped")
	}
	if n := strings.Count(got, "\n"); n != 1 {
		t.Errorf("rendered %d lines, want 1 unbroken overflow", n)
	}
}

// TestCopy runs the reader loop over the fixture and pins the whole render.
// Regenerate both goldens with:
//
//	go test ./internal/logfmt -run TestCopy -update
func TestCopy(t *testing.T) {
	in, err := os.ReadFile(filepath.Join("testdata", "stream.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		color bool
		file  string
	}{
		{name: "plain", file: "stream.txt"},
		{name: "ansi", color: true, file: "stream.ansi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			f := New(Options{Color: tc.color, Width: 100, MsgWidth: 24, Hide: []string{"component"}})
			if err := f.Copy(&buf, bytes.NewReader(in)); err != nil {
				t.Fatalf("Copy: %v", err)
			}
			path := filepath.Join("testdata", tc.file)
			if *updateGoldens {
				if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update to write goldens)", err)
			}
			if !bytes.Equal(buf.Bytes(), want) {
				t.Errorf("render diverges from %s:\n%s", tc.file, buf.String())
			}
		})
	}
}

var (
	ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")
	// attrRe finds the fold points: a rendered line may only overflow the
	// wrap column when it carries a single, unbreakable attribute.
	attrRe = regexp.MustCompile(`(^| )[A-Za-z_][A-Za-z0-9_.]*=`)
)

// TestCopyWrapsWithinWidth is the invariant behind the whole layout: every
// emitted line fits the wrap column once the escapes are stripped, unless it
// carries a single token that cannot fit on any line. A golden only catches
// the cases it happens to hold; this catches the class.
func TestCopyWrapsWithinWidth(t *testing.T) {
	in, err := os.ReadFile(filepath.Join("testdata", "stream.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{60, 80, 100, 160} {
		var buf bytes.Buffer
		f := New(Options{Color: true, Width: width})
		if err := f.Copy(&buf, bytes.NewReader(in)); err != nil {
			t.Fatalf("Copy: %v", err)
		}
		for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
			plain := ansiRe.ReplaceAllString(line, "")
			if utf8.RuneCountInString(plain) <= width {
				continue
			}
			// A single attribute that cannot fit any line is allowed to
			// overflow alone; two on one line means the fold gave up.
			if n := len(attrRe.FindAllString(plain, -1)); n > 1 {
				t.Errorf("width %d: line overflows carrying %d attributes:\n%s", width, n, plain)
			}
		}
	}
}

// TestCopyRejectsOversizedLine documents the one input Copy refuses: a record
// beyond maxLine, far above journald's own per-line cap.
func TestCopyRejectsOversizedLine(t *testing.T) {
	huge := `{"level":"INFO","msg":"m","x":"` + strings.Repeat("y", maxLine+1) + `"}`
	err := New(Options{}).Copy(&bytes.Buffer{}, strings.NewReader(huge))
	if err == nil {
		t.Fatal("Copy accepted a line beyond maxLine, want an error")
	}
}

// wideRecord builds a record with n attributes, like the startup config dump.
func wideRecord(n int) string {
	var b strings.Builder
	b.WriteString(`{"level":"INFO","msg":"configuration"`)
	for i := range n {
		fmt.Fprintf(&b, `,"k%02d":%d`, i, i)
	}
	b.WriteString("}")
	return b.String()
}
