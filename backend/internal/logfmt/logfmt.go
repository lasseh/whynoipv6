// Package logfmt renders the JSON slog records the binaries write (09-ops.md
// §13) into aligned, level-colored terminal lines: one line per record, with
// the attributes wrapped to the terminal width under a hanging indent.
//
// It imports no other internal/ package and nothing outside the standard
// library. Display width means utf8.RuneCountInString — CJK and emoji
// double-width cells are out of scope, and the only realistic exposure is err
// text, since hostnames reach the log punycode-canonical.
package logfmt

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"
)

// The record keys with fixed meaning in every line (09-ops.md §13).
const (
	keyTime  = "time"
	keyLevel = "level"
	keyMsg   = "msg"
	keyRunID = "run_id"
	keyErr   = "err"
)

// Rendering geometry. stampLayout drops the date, which journald and the unit
// name already establish, and pins the fraction to three digits: slog formats
// with time.RFC3339Nano, which trims trailing zeros, so the raw stamp varies
// in width from line to line and cannot carry a column.
const (
	stampLayout = "15:04:05.000"
	stampWidth  = len(stampLayout)
	levelWidth  = 5

	defaultMsgWidth = 24
	defaultWidth    = 120
	minWidth        = 60

	runIDWidth = 8

	// A record wider than collapseAbove attributes is a config dump, not a
	// log line: render collapseKeep of them and count the rest.
	collapseAbove = 20
	collapseKeep  = 3

	// maxLine bounds one record; journald's own LineMax is far below it.
	maxLine = 1 << 20
)

// ANSI escapes. Levels are colored by threshold rather than by equality, so
// the DEBUG+2 and ERROR+4 forms slog emits still land in the right bucket.
const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiAmber   = "\x1b[33m"
	ansiCyan    = "\x1b[36m"
	ansiDim     = "\x1b[90m"
	ansiBoldRed = "\x1b[1;31m"
)

// Options configures a Formatter. The zero value is usable: no color, no level
// filtering, default widths.
type Options struct {
	// Color emits ANSI escapes. The caller decides, since only it knows
	// whether the destination is a terminal.
	Color bool
	// Full keeps the raw timestamp and the full run_id, and never collapses
	// a wide record.
	Full bool
	// Hide drops these attribute keys from every record.
	Hide []string
	// MinLevel skips records below it. Nil means no filtering — a plain
	// slog.Level cannot express that, since slog.LevelInfo is the zero
	// value and would silently drop every debug record.
	MinLevel slog.Leveler
	// MsgWidth is the message column; a longer message pushes its
	// attributes right rather than being truncated.
	MsgWidth int
	// Width is the wrap column, clamped up to minWidth.
	Width int
}

// Formatter renders records under one set of options. It is stateless across
// records and safe to reuse; it is not safe for concurrent use.
type Formatter struct {
	color    bool
	full     bool
	hide     map[string]struct{}
	minLevel slog.Level
	msgWidth int
	width    int
}

// New returns a Formatter with the defaults applied once, so no per-record
// path has to branch on a zero value.
func New(opts Options) *Formatter {
	f := &Formatter{
		color:    opts.Color,
		full:     opts.Full,
		msgWidth: opts.MsgWidth,
		width:    opts.Width,
		minLevel: slog.LevelDebug - 4, // below every level slog emits
	}
	if opts.MinLevel != nil {
		f.minLevel = opts.MinLevel.Level()
	}
	if f.msgWidth <= 0 {
		f.msgWidth = defaultMsgWidth
	}
	switch {
	case f.width <= 0:
		f.width = defaultWidth
	case f.width < minWidth:
		f.width = minWidth
	}
	if len(opts.Hide) > 0 {
		f.hide = make(map[string]struct{}, len(opts.Hide))
		for _, k := range opts.Hide {
			if k != "" {
				f.hide[k] = struct{}{}
			}
		}
	}
	return f
}

// kv is one rendered attribute: plain carries the display width, colored the
// bytes to emit. Folding measures plain, or the escapes would skew the wrap.
type kv struct {
	plain   string
	colored string
}

// record is one decoded log line.
type record struct {
	stamp string
	level slog.Level
	text  string // the level as written, e.g. "INFO" or "ERROR+4"
	msg   string
	attrs []kv
}

// Append renders one raw line, including its trailing newline, onto dst and
// reports whether anything was appended. A line that is not a JSON object
// passes through dimmed rather than being dropped: journalctl interleaves
// systemd's own unit lines with the binary's stdout, and a Go panic never
// arrives as slog. The returned slice may not alias dst.
func (f *Formatter) Append(dst, raw []byte) ([]byte, bool) {
	line := bytes.TrimRight(raw, "\r")
	if len(bytes.TrimSpace(line)) == 0 {
		return dst, false
	}
	rec, ok := f.decode(line)
	if !ok {
		// Passthrough skips the level filter: it has no level to test.
		dst = f.appendColored(dst, ansiDim, string(line))
		return append(dst, '\n'), true
	}
	if rec.level < f.minLevel {
		return dst, false
	}
	return f.appendRecord(dst, &rec), true
}

// Copy renders every line of src onto dst. It does not buffer: the caller
// passes os.Stdout to follow a stream, or a bufio.Writer for a bulk read, and
// owns the flush on the error path as well as on success.
func (f *Formatter) Copy(dst io.Writer, src io.Reader) error {
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxLine)
	buf := make([]byte, 0, 512)
	for sc.Scan() {
		out, ok := f.Append(buf[:0], sc.Bytes())
		if !ok {
			continue
		}
		buf = out
		if _, err := dst.Write(buf); err != nil {
			return fmt.Errorf("write: %w", err)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read: %w", err)
	}
	return nil
}

// decode walks the object with a token loop rather than unmarshalling into a
// map: slog does not dedupe keys, so a call site that passes an attr the
// child logger already stamped (run_id, cmd/crawler/main.go) emits it twice,
// and a map would silently drop one. The loop is lossless and keeps the order
// the handler wrote, which is meaningful for the config summary.
func (f *Formatter) decode(line []byte) (record, bool) {
	dec := json.NewDecoder(bytes.NewReader(line))
	tok, err := dec.Token()
	if err != nil {
		return record{}, false
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return record{}, false
	}
	rec := record{attrs: make([]kv, 0, 8)}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return record{}, false
		}
		key, ok := tok.(string)
		if !ok {
			return record{}, false
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return record{}, false
		}
		f.field(&rec, key, val)
	}
	if _, err := dec.Token(); err != nil { // the closing brace
		return record{}, false
	}
	return rec, true
}

// field routes one decoded key onto the record.
func (f *Formatter) field(rec *record, key string, val json.RawMessage) {
	switch key {
	case keyTime:
		rec.stamp = f.stamp(display(val))
		return
	case keyLevel:
		rec.text = display(val)
		if err := rec.level.UnmarshalText([]byte(rec.text)); err != nil {
			rec.level = slog.LevelInfo
		}
		return
	case keyMsg:
		rec.msg = display(val)
		return
	}
	if _, hidden := f.hide[key]; hidden {
		return
	}
	text := display(val)
	if key == keyRunID && !f.full && len(text) > runIDWidth {
		text = text[:runIDWidth]
	}
	rec.attrs = append(rec.attrs, f.attr(key, text))
}

// stamp reduces the record's RFC3339 time to a fixed-width wall clock. A
// missing or unparseable time pads the column instead of collapsing it, so
// one malformed record cannot shift the whole stream.
func (f *Formatter) stamp(s string) string {
	if f.full {
		return s
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return strings.Repeat(" ", stampWidth)
	}
	return t.Format(stampLayout)
}

// attr builds the plain and colored forms of one key=value token.
func (f *Formatter) attr(key, text string) kv {
	a := kv{plain: key + "=" + text}
	if !f.color {
		a.colored = a.plain
		return a
	}
	value := text
	if key == keyErr {
		value = ansiRed + text + ansiReset
	}
	a.colored = ansiCyan + key + ansiReset + ansiDim + "=" + ansiReset + value
	return a
}

// appendRecord lays out one record: the fixed head, then the attributes
// folded to the wrap column under a hanging indent.
func (f *Formatter) appendRecord(dst []byte, rec *record) []byte {
	level := pad(rec.text, levelWidth)
	msgWidth := f.msgWidth
	attrs := f.collapse(rec.attrs)
	msg := rec.msg
	if len(attrs) > 0 {
		msg = pad(rec.msg, msgWidth)
	}
	indent := utf8.RuneCountInString(rec.stamp) + 1 +
		utf8.RuneCountInString(level) + 1 + utf8.RuneCountInString(msg)

	dst = f.appendColored(dst, ansiDim, rec.stamp)
	dst = append(dst, ' ')
	dst = f.appendColored(dst, levelColor(rec.level), level)
	dst = append(dst, ' ')
	dst = f.appendColored(dst, ansiBold, msg)

	col := indent
	for _, a := range attrs {
		width := utf8.RuneCountInString(a.plain)
		// Never split a token: an oversized value overflows alone rather
		// than looping forever on a column it can never fit.
		if col > indent && col+1+width > f.width {
			dst = append(dst, '\n')
			dst = append(dst, strings.Repeat(" ", indent)...)
			col = indent
		}
		dst = append(dst, ' ')
		dst = append(dst, a.colored...)
		col += 1 + width
	}
	return append(dst, '\n')
}

// collapse shortens a record too wide to read — the startup config summary is
// 71 attributes on one line — to its first few plus a count.
func (f *Formatter) collapse(attrs []kv) []kv {
	if f.full || len(attrs) <= collapseAbove {
		return attrs
	}
	rest := fmt.Sprintf("… +%d more (--full)", len(attrs)-collapseKeep)
	out := make([]kv, 0, collapseKeep+1)
	out = append(out, attrs[:collapseKeep]...)
	return append(out, kv{plain: rest, colored: f.dim(rest)})
}

// appendColored writes s wrapped in code when color is on.
func (f *Formatter) appendColored(dst []byte, code, s string) []byte {
	if !f.color || s == "" {
		return append(dst, s...)
	}
	dst = append(dst, code...)
	dst = append(dst, s...)
	return append(dst, ansiReset...)
}

// dim returns s dimmed when color is on.
func (f *Formatter) dim(s string) string {
	if !f.color {
		return s
	}
	return ansiDim + s + ansiReset
}

// levelColor buckets by threshold so the offset forms (ERROR+4) still color.
func levelColor(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return ansiBoldRed
	case l >= slog.LevelWarn:
		return ansiAmber
	case l >= slog.LevelInfo:
		return ansiGreen
	default:
		return ansiDim
	}
}

// display renders one JSON value for a terminal: strings unquoted, everything
// else as written. Control characters are escaped either way, because a pgx
// error carries real newlines and one record must stay one line.
func display(raw json.RawMessage) string {
	s := string(raw)
	if len(raw) > 0 && raw[0] == '"' {
		var unquoted string
		if err := json.Unmarshal(raw, &unquoted); err == nil {
			s = unquoted
		}
	}
	return escape(s)
}

// escape replaces the control characters that would break the line.
func escape(s string) string {
	if strings.IndexFunc(s, func(r rune) bool { return r < ' ' || r == 0x7f }) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < ' ' || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// pad right-pads s to at least n display columns.
func pad(s string, n int) string {
	if w := utf8.RuneCountInString(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}
