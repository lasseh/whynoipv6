package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/term"

	"github.com/spf13/cobra"

	"github.com/lasseh/whynoipv6/internal/logfmt"
)

const journalctl = "journalctl"

// journalUnits maps the short name an operator types to the unit file in
// deploy/systemd. A prefix rule would be wrong: the naming is not uniform
// across the fleet (v6ctl-geoip-update.timer), so this stays a table. Any
// name carrying a "." or "@" is passed to journalctl untouched.
var journalUnits = map[string]string{
	"api":           "whynoipv6-api.service",
	"crawler":       "whynoipv6-crawler.service",
	"export":        "whynoipv6-export.service",
	"unbound-stats": "whynoipv6-unbound-stats.service",
}

// logsFlags is the flag set of `v6ctl logs`.
type logsFlags struct {
	unit   string
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
	flags.StringVarP(&f.unit, "unit", "u", "crawler", "unit to read (api|crawler|export|unbound-stats, or a unit name)")
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

	if records, finite := stdinRecords(); stdin || records {
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
	jc := exec.CommandContext(cmd.Context(), journalctl, journalctlArgs(f, extra)...)
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
	written, renderErr := render(formatter, pipe, !f.follow)
	waitErr := jc.Wait()

	// Ctrl-C reaches journalctl directly as a member of the terminal's
	// process group, so a signalled exit under a cancelled context is the
	// normal way --follow ends, not a failure.
	if cmd.Context().Err() != nil {
		return nil
	}
	if renderErr != nil {
		return renderErr
	}
	if waitErr != nil {
		return fmt.Errorf("%s: %w", journalctl, waitErr)
	}
	if written == 0 && !f.follow {
		fmt.Fprintf(os.Stderr, "no records matched (unit %s)\n", resolveUnit(f.unit))
	}
	return nil
}

// journalctlArgs builds the child's argv. `-o cat` is ours to set: journald's
// own line prefix would break the parse. Kept pure so it can be tested.
func journalctlArgs(f *logsFlags, extra []string) []string {
	args := make([]string, 0, 12+len(extra))
	args = append(args, "--unit", resolveUnit(f.unit), "--output", "cat", "--no-pager")
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

// resolveUnit expands a short name; anything that already looks like a unit
// name is left alone.
func resolveUnit(name string) string {
	if unit, ok := journalUnits[name]; ok {
		return unit
	}
	return name
}

// render copies src through the formatter to stdout. Buffering is decided by
// the source, not by --follow: a finite read (the journal, a file) is worth
// buffering, while anything live must reach the terminal as it arrives, or a
// `docker compose logs -f … | v6ctl logs -` pipe goes silent for whole 4 KB
// chunks and reads as a hang.
func render(formatter *logfmt.Formatter, src io.Reader, finite bool) (int64, error) {
	counter := &countingWriter{w: os.Stdout}
	err := func() error {
		if !finite {
			return formatter.Copy(counter, src)
		}
		buffered := bufio.NewWriter(counter)
		copyErr := formatter.Copy(buffered, src)
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

// countingWriter counts the bytes written, for the zero-match hint.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
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
