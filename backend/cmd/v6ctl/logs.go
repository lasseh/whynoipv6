package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"

	"golang.org/x/term"

	"github.com/spf13/cobra"

	"github.com/lasseh/whynoipv6/internal/logfmt"
)

const journalctl = "journalctl"

// journalUnits maps the short name an operator types to a journalctl --unit
// pattern. They are globs on purpose: a service run more than once per host
// is a template, so the crawler is whynoipv6-crawler@1.service and
// whynoipv6-crawler@2.service rather than the plain unit the repo's
// deploy/systemd/ ships, and one pattern covers both shapes. A prefix rule
// over all of them would still be wrong — the fleet's naming is not uniform
// (v6ctl-geoip-update.timer) — so this stays a table. Anything not in it
// reaches journalctl untouched, globs included.
var journalUnits = map[string]string{
	"api":           "whynoipv6-api*.service",
	"crawler":       "whynoipv6-crawler*.service",
	"export":        "whynoipv6-export*.service",
	"unbound-stats": "whynoipv6-unbound-stats*.service",
}

// logsFlags is the flag set of `v6ctl logs`.
type logsFlags struct {
	units  []string
	lines  int
	follow bool
	since  string
	until  string
	level  string
	hide   []string
	full   bool
	width  int
	color  string
}

// logsCmd pretty-prints the JSON logs of 09-ops.md §13, from journald or from
// stdin. It overrides the root PersistentPreRunE: reading logs needs no
// database, and the root hook would print its own config summary into the
// stream being read.
func logsCmd() *cobra.Command {
	f := &logsFlags{}
	cmd := &cobra.Command{
		Use:   "logs [-] [-- journalctl-args…]",
		Short: "Pretty-print the JSON logs (journald or stdin)",
		Long: "Pretty-print the JSON logs (journald or stdin).\n\n" +
			"Reads stdin when it is piped or when given `-`, otherwise runs journalctl\n" +
			"for the unit. Levels are filtered here, not by journald: the binaries write\n" +
			"plain stdout, so every record lands at the same syslog priority and\n" +
			"`journalctl -p err` returns everything. Use --level instead.",
		Args:              cobra.ArbitraryArgs,
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogs(cmd, f, args)
		},
	}
	flags := cmd.Flags()
	flags.StringSliceVarP(&f.units, "unit", "u", []string{"crawler"},
		"units to read; repeatable, and a unit name may be a glob (api|crawler|export|unbound-stats expand to one)")
	flags.IntVarP(&f.lines, "lines", "n", 1000, "records to read from the journal (0 = no limit)")
	flags.BoolVarP(&f.follow, "follow", "f", false, "keep reading as records arrive")
	flags.StringVar(&f.since, "since", "", "journalctl --since expression")
	flags.StringVar(&f.until, "until", "", "journalctl --until expression")
	flags.StringVar(&f.level, "level", "", "drop records below this level (debug|info|warn|error)")
	flags.StringSliceVar(&f.hide, "hide", nil, "attribute keys to drop from every record")
	flags.BoolVar(&f.full, "full", false, "raw timestamps, whole run_id, no collapsed records")
	flags.IntVar(&f.width, "width", 0, "wrap column (default: the terminal width)")
	flags.StringVar(&f.color, "color", "auto", "colorize: auto|always|never")
	return cmd
}

// runLogs wires the formatter to its source: stdin when piped or asked for,
// journalctl otherwise.
func runLogs(cmd *cobra.Command, f *logsFlags, args []string) error {
	opts, err := f.options()
	if err != nil {
		return err
	}
	stdin, extra := splitStdinArg(args)

	// A reader that goes away mid-stream (`v6ctl logs | head`) must reach
	// render as EPIPE on the write, which it treats as a normal end; without
	// this the runtime exits the process on SIGPIPE for a stdout write.
	signal.Ignore(syscall.SIGPIPE)

	if records, finite := stdinRecords(); stdin || records {
		if !stdin && !finite {
			// The invocation read like a journal query; say why it is not
			// one, or a `--unit` given under a piped stdin looks ignored.
			fmt.Fprintln(os.Stderr, "reading records from stdin (a pipe), not the journal; pass `-` to make that explicit")
		}
		// Only a regular file is finite; a pipe is somebody streaming into
		// us and must not sit in a buffer.
		_, err := render(logfmt.New(opts), os.Stdin, finite)
		return err
	}
	opts.Hide = append(opts.Hide, hiddenInJournalMode(f)...)
	return runJournalctl(cmd, logfmt.New(opts), f, extra)
}

// hiddenInJournalMode drops the attributes the journalctl invocation already
// establishes. component names the unit that was asked for; a piped-in stream
// may carry several, so this applies to the journalctl path only.
func hiddenInJournalMode(f *logsFlags) []string {
	if f.full {
		return nil
	}
	return []string{"component"}
}

// options turns the flags into formatter options.
func (f *logsFlags) options() (logfmt.Options, error) {
	color, err := useColor(f.color)
	if err != nil {
		return logfmt.Options{}, err
	}
	level, err := parseLogLevel(f.level)
	if err != nil {
		return logfmt.Options{}, err
	}
	return logfmt.Options{
		Color:    color,
		Full:     f.full,
		Hide:     f.hide,
		MinLevel: level,
		Width:    wrapWidth(f.width),
	}, nil
}

// runJournalctl streams the unit's journal through the formatter.
func runJournalctl(cmd *cobra.Command, formatter *logfmt.Formatter, f *logsFlags, extra []string) error {
	if _, err := exec.LookPath(journalctl); err != nil {
		return fmt.Errorf("%s not found; pipe the records in instead, e.g. "+
			"`docker compose logs -f --no-log-prefix crawler | v6ctl logs -`", journalctl)
	}
	jc := exec.CommandContext(cmd.Context(), journalctl, journalctlArgs(f, extra)...) //nolint:gosec // the operator's own journalctl flags
	// journalctl's own diagnostics stay on stderr: merged into stdout they
	// would reach the formatter and read like log content.
	jc.Stderr = os.Stderr
	pipe, err := jc.StdoutPipe()
	if err != nil {
		return fmt.Errorf("%s: %w", journalctl, err)
	}
	if err := jc.Start(); err != nil {
		return fmt.Errorf("%s: %w", journalctl, err)
	}
	read, renderErr := render(formatter, pipe, !f.follow)
	waitErr := jc.Wait()

	// Ctrl-C reaches journalctl directly as a member of the terminal's
	// process group, so a signalled exit under a cancelled context is the
	// normal way --follow ends, not a failure.
	if cmd.Context().Err() != nil {
		return nil //nolint:nilerr // Ctrl-C reached journalctl too: a signalled exit is the normal end of --follow
	}
	if renderErr != nil {
		return renderErr
	}
	if waitErr != nil {
		return fmt.Errorf("%s: %w", journalctl, waitErr)
	}
	if read == 0 && !f.follow {
		fmt.Fprintf(os.Stderr, "no records matched %v; `systemctl list-units 'whynoipv6-*'` "+
			"lists what this host actually runs\n", resolveUnits(f.units))
	}
	return nil
}

// journalctlArgs builds the child's argv. `-o cat` is ours to set: journald's
// own line prefix would break the parse. Kept pure so it can be tested.
func journalctlArgs(f *logsFlags, extra []string) []string {
	args := make([]string, 0, 12+len(extra))
	for _, unit := range resolveUnits(f.units) {
		args = append(args, "--unit", unit)
	}
	args = append(args, "--output", "cat", "--no-pager")
	if f.follow {
		args = append(args, "--follow")
	}
	if f.lines > 0 {
		args = append(args, "--lines", strconv.Itoa(f.lines))
	}
	if f.since != "" {
		args = append(args, "--since", f.since)
	}
	if f.until != "" {
		args = append(args, "--until", f.until)
	}
	return append(args, extra...)
}

// resolveUnits expands the short names; anything else is left alone, so a
// glob or a full unit name reaches journalctl as written.
func resolveUnits(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if unit, ok := journalUnits[name]; ok {
			name = unit
		}
		out = append(out, name)
	}
	return out
}

// render copies src through the formatter to stdout. Buffering is decided by
// the source, not by --follow: a finite read (the journal, a file) is worth
// buffering, while anything live must reach the terminal as it arrives, or a
// `docker compose logs -f … | v6ctl logs -` pipe goes silent for whole 4 KB
// chunks and reads as a hang.
func render(formatter *logfmt.Formatter, src io.Reader, finite bool) (int64, error) {
	counter := &countingReader{r: src}
	err := func() error {
		if !finite {
			return formatter.Copy(os.Stdout, counter)
		}
		buffered := bufio.NewWriter(os.Stdout)
		copyErr := formatter.Copy(buffered, counter)
		// Flush on the error path too, or an interrupted bulk read drops
		// the records already rendered.
		if flushErr := buffered.Flush(); copyErr == nil {
			copyErr = flushErr
		}
		return copyErr
	}()
	if errors.Is(err, syscall.EPIPE) {
		return counter.n, nil // `v6ctl logs | head`
	}
	return counter.n, err
}

// countingReader counts the bytes read, for the zero-match hint: a record
// that --level dropped was still a match, so the output side cannot tell.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// splitStdinArg pulls the "-" marker out of the positional arguments; what is
// left is forwarded to journalctl verbatim.
func splitStdinArg(args []string) (stdin bool, extra []string) {
	extra = make([]string, 0, len(args))
	for _, a := range args {
		if a == "-" {
			stdin = true
			continue
		}
		extra = append(extra, a)
	}
	return stdin, extra
}

// stdinRecords reports whether stdin carries records, and whether that source
// is finite. Only a pipe or a file counts as records: under systemd and in CI
// stdin is /dev/null, which is not a character device either, and reading it
// would print nothing at all instead of running journalctl.
func stdinRecords() (records, finite bool) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false, false
	}
	mode := info.Mode()
	return mode&os.ModeNamedPipe != 0 || mode.IsRegular(), mode.IsRegular()
}

// useColor resolves the --color mode. Auto looks at stdout, the descriptor
// the escapes travel on.
func useColor(mode string) (bool, error) {
	switch mode {
	case "always":
		return true, nil
	case "never":
		return false, nil
	case "auto", "":
		if os.Getenv("NO_COLOR") != "" {
			return false, nil
		}
		return term.IsTerminal(int(os.Stdout.Fd())), nil
	default:
		return false, fmt.Errorf("--color: unknown mode %q (auto|always|never)", mode)
	}
}

// parseLogLevel accepts what slog itself writes and what an operator types;
// an empty value means no filtering.
func parseLogLevel(s string) (slog.Leveler, error) {
	if s == "" {
		return nil, nil
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(s)); err != nil {
		return nil, fmt.Errorf("--level: %w", err)
	}
	return level, nil
}

// wrapWidth asks stderr before stdout: under `v6ctl logs | less -R` stdout is
// a pipe, but stderr is still the terminal and its width is the one less
// renders at. Zero lets the formatter apply its own default.
func wrapWidth(explicit int) int {
	if explicit > 0 {
		return explicit
	}
	for _, f := range []*os.File{os.Stderr, os.Stdout} {
		if width, _, err := term.GetSize(int(f.Fd())); err == nil && width > 0 {
			return width
		}
	}
	return 0
}
