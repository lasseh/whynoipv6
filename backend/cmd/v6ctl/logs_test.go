package main

import (
	"log/slog"
	"slices"
	"strings"
	"testing"
)

// TestJournalctlArgs pins the child argv: `-o cat` and `--no-pager` are ours
// to set (journald's line prefix breaks the parse), short unit names expand,
// and anything after `--` is forwarded untouched.
func TestJournalctlArgs(t *testing.T) {
	cases := []struct {
		name  string
		flags logsFlags
		extra []string
		want  []string
		skip  []string
	}{
		{
			name:  "defaults",
			flags: logsFlags{units: []string{"crawler"}, lines: 1000},
			want:  []string{"--unit", "whynoipv6-crawler*.service", "--output", "cat", "--no-pager", "--lines", "1000"},
			skip:  []string{"--follow", "--since"},
		},
		{
			name:  "an unknown unit name passes through",
			flags: logsFlags{units: []string{"whynoipv6-notify@export.service"}},
			want:  []string{"--unit", "whynoipv6-notify@export.service"},
		},
		{
			name:  "a glob passes through untouched",
			flags: logsFlags{units: []string{"whynoipv6-crawler@[12].service"}},
			want:  []string{"--unit", "whynoipv6-crawler@[12].service"},
		},
		{
			name:  "no limit drops --lines",
			flags: logsFlags{units: []string{"api"}, lines: 0},
			want:  []string{"--unit", "whynoipv6-api*.service"},
			skip:  []string{"--lines"},
		},
		{
			name:  "follow, since and until",
			flags: logsFlags{units: []string{"crawler"}, follow: true, since: "1 hour ago", until: "now"},
			want:  []string{"--follow", "--since", "1 hour ago", "--until", "now"},
		},
		{
			name:  "extra arguments are appended verbatim",
			flags: logsFlags{units: []string{"crawler"}},
			extra: []string{"--grep", "preflight"},
			want:  []string{"--grep", "preflight"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := journalctlArgs(&tc.flags, tc.extra)
			joined := strings.Join(got, "\x00")
			for _, want := range tc.want {
				if !slices.Contains(got, want) {
					t.Errorf("missing %q in %v", want, got)
				}
			}
			for _, skip := range tc.skip {
				if slices.Contains(got, skip) {
					t.Errorf("unexpected %q in %v", skip, got)
				}
			}
			if !strings.Contains(joined, "--output\x00cat") {
				t.Errorf("-o cat is mandatory: %v", got)
			}
		})
	}
}

// TestJournalctlArgsMultipleUnits: -u is repeatable, and each unit needs its
// own --unit flag. Two crawler processes on one host run as a template, so
// the crawler alias is a glob and a reader may want several units at once.
func TestJournalctlArgsMultipleUnits(t *testing.T) {
	got := journalctlArgs(&logsFlags{units: []string{"api", "crawler"}}, nil)
	joined := strings.Join(got, "\x00")
	for _, want := range []string{"--unit\x00whynoipv6-api*.service", "--unit\x00whynoipv6-crawler*.service"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %v", strings.ReplaceAll(want, "\x00", " "), got)
		}
	}
	if n := strings.Count(joined, "--unit"); n != 2 {
		t.Errorf("got %d --unit flags, want 2: %v", n, got)
	}
}

// TestSplitStdinArg: "-" selects stdin and never reaches journalctl.
func TestSplitStdinArg(t *testing.T) {
	stdin, extra := splitStdinArg([]string{"-", "--grep", "x"})
	if !stdin {
		t.Error(`"-" did not select stdin`)
	}
	if !slices.Equal(extra, []string{"--grep", "x"}) {
		t.Errorf("extra = %v", extra)
	}
	if stdin, extra = splitStdinArg(nil); stdin || len(extra) != 0 {
		t.Errorf("no arguments: stdin=%v extra=%v", stdin, extra)
	}
}

// TestParseLogLevel: empty means no filtering, and both the operator's
// spelling and slog's own offset forms parse.
func TestParseLogLevel(t *testing.T) {
	level, err := parseLogLevel("")
	if err != nil || level != nil {
		t.Fatalf("empty --level: %v, %v", level, err)
	}
	for _, in := range []string{"warn", "WARN", "error+4"} {
		if _, err := parseLogLevel(in); err != nil {
			t.Errorf("parseLogLevel(%q): %v", in, err)
		}
	}
	if level, err = parseLogLevel("warn"); err != nil || level.Level() != slog.LevelWarn {
		t.Errorf(`parseLogLevel("warn") = %v, %v`, level, err)
	}
	if _, err := parseLogLevel("verbose"); err == nil {
		t.Error("parseLogLevel(\"verbose\"): want an error")
	}
}

// TestUseColor covers the three modes and the NO_COLOR override; auto under
// `go test` sees a non-terminal stdout.
func TestUseColor(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want bool
	}{{"always", true}, {"never", false}, {"auto", false}, {"", false}} {
		got, err := useColor(tc.mode)
		if err != nil {
			t.Fatalf("useColor(%q): %v", tc.mode, err)
		}
		if got != tc.want {
			t.Errorf("useColor(%q) = %v, want %v", tc.mode, got, tc.want)
		}
	}
	t.Setenv("NO_COLOR", "1")
	if got, _ := useColor("auto"); got {
		t.Error("NO_COLOR did not disable auto color")
	}
	if got, _ := useColor("always"); !got {
		t.Error("NO_COLOR should not override an explicit --color=always")
	}
	if _, err := useColor("rainbow"); err == nil {
		t.Error("useColor(\"rainbow\"): want an error")
	}
}

// TestHiddenInJournalMode: component is only redundant when this command
// picked the unit; a piped-in stream may carry several.
func TestHiddenInJournalMode(t *testing.T) {
	if got := hiddenInJournalMode(&logsFlags{}); !slices.Equal(got, []string{"component"}) {
		t.Errorf("journalctl mode hides %v, want [component]", got)
	}
	if got := hiddenInJournalMode(&logsFlags{full: true}); got != nil {
		t.Errorf("--full hides %v, want nothing", got)
	}
}

// TestLogsCmdSkipsRootHook: the no-op override is what lets `logs` run with
// no DATABASE_URL, like geoip and campaign validate.
func TestLogsCmdSkipsRootHook(t *testing.T) {
	if logsCmd().PersistentPreRunE == nil {
		t.Fatal("logs must override the root PersistentPreRunE (it needs no database)")
	}
	if err := logsCmd().PersistentPreRunE(nil, nil); err != nil {
		t.Errorf("the override must be a no-op: %v", err)
	}
}
