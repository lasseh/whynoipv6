package checker

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

const maxSMTPAttempts = 3

// SMTPIPv6 checks whether the domain's MX accepts SMTP over IPv6.
type SMTPIPv6 struct {
	dialer *SafeDialer
}

// NewSMTPIPv6 creates a new smtp_ipv6 checker.
func NewSMTPIPv6(dialer *SafeDialer) *SMTPIPv6 {
	return &SMTPIPv6{dialer: dialer}
}

func (c *SMTPIPv6) Name() string { return NameSMTP }
func (c *SMTPIPv6) Check(ctx context.Context, domain string, kind Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	d := &SMTPDetail{}

	// Lookup MX records.
	mxRecords, _, err := c.dialer.Resolver().LookupMX(ctx, domain)
	if err != nil {
		d.Error = err.Error()
		return Result{Status: StatusError, Detail: d, Latency: time.Since(start)}, nil
	}

	if len(mxRecords) == 0 {
		d.Reason = "no MX records"
		return Result{
			Status:  StatusNotApplicable,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	// Sort by preference.
	sort.Slice(mxRecords, func(i, j int) bool {
		return mxRecords[i].Preference < mxRecords[j].Preference
	})

	attempts := min(len(mxRecords), maxSMTPAttempts)

	var lastErr error
	for i := 0; i < attempts; i++ {
		mx := mxRecords[i]
		result, tryErr := c.tryMX(ctx, mx.Mx, mx.Preference)
		if tryErr == nil {
			result.Latency = time.Since(start)
			return result, nil
		}
		lastErr = tryErr
		if ctx.Err() != nil {
			break
		}
	}

	if lastErr != nil {
		if isConnRefused(lastErr) {
			d.Error = errConnRefused
			return Result{Status: StatusUnsupported, Detail: d, Latency: time.Since(start)}, nil
		}
		d.Error = lastErr.Error()
	}

	return Result{
		Status:  StatusUnsupported,
		Detail:  d,
		Latency: time.Since(start),
	}, nil
}

func (c *SMTPIPv6) tryMX(ctx context.Context, mxHost string, preference uint16) (Result, error) {
	// Resolve AAAA for MX host.
	ips, _, _, _, err := c.dialer.Resolver().LookupAAAA(ctx, mxHost)
	if err != nil || len(ips) == 0 {
		return Result{}, fmt.Errorf("no AAAA record for MX host %s", mxHost)
	}

	ip := ips[0]
	if err := c.dialer.ValidateIP(ip); err != nil {
		return Result{}, fmt.Errorf("MX host %s address blocked: %w", mxHost, err)
	}

	// Connect via TCP6 to port 25.
	addr := net.JoinHostPort(ip.String(), "25")
	conn, err := c.dialer.dialer.DialContext(ctx, "tcp6", addr)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = conn.Close() }()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	reader := bufio.NewReader(conn)

	// Read banner.
	banner, err := reader.ReadString('\n')
	if err != nil {
		return Result{}, fmt.Errorf("reading SMTP banner: %w", err)
	}
	banner = strings.TrimSpace(banner)

	d := &SMTPDetail{
		MXHost:       mxHost,
		MXPreference: &preference,
		Address:      ip.String(),
		Banner:       banner,
	}

	if !strings.HasPrefix(banner, "220") {
		d.Error = "unexpected banner"
		return Result{Status: StatusUnsupported, Detail: d}, nil
	}

	// Send EHLO.
	_, err = fmt.Fprintf(conn, "EHLO whynoipv6.com\r\n")
	if err != nil {
		d.Error = fmt.Sprintf("EHLO write failed: %v", err)
		return Result{Status: StatusPartial, Detail: d}, nil
	}

	// Read EHLO response (multi-line: 250-... until 250 ...).
	// Cap at 100 lines to prevent malicious servers from sending unbounded data.
	const maxEHLOLines = 100
	var ehloLines []string
	for range maxEHLOLines {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			break
		}
		line = strings.TrimSpace(line)
		ehloLines = append(ehloLines, line)
		// Last line of EHLO response has "250 " (space, not dash).
		if len(line) >= 4 && line[3] == ' ' {
			break
		}
	}

	ehloResponse := strings.Join(ehloLines, "\n")
	d.EHLOResponse = ehloResponse
	starttls := strings.Contains(strings.ToUpper(ehloResponse), "STARTTLS")
	d.STARTTLSOffered = &starttls

	// Send QUIT.
	_, _ = fmt.Fprintf(conn, "QUIT\r\n")

	return Result{
		Status: StatusSupported,
		Detail: d,
	}, nil
}
