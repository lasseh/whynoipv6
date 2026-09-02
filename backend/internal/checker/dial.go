package checker

import (
	"context"
	"net"
	"time"
)

// dialOverAAAA is the shared http_ipv6/https_ipv6 attempt loop
// (01-engine.md §11.6/§11.7): resolve AAAA (a resolver failure is transient
// — the consensus quorum already confirmed AAAA exists — so it must not
// produce a definitive unsupported observation; no records → unsupported),
// validate and try up to three addresses, then classify the terminal error
// exactly the same way on both paths so the conn composition applies
// identically: connection refused → unsupported, timeout → error, TLS
// (withTLS only — no certificate branch exists on port 80) → unsupported,
// anything else → error.
//
//nolint:unparam // error is always nil but kept so Check bodies stay a single return
func dialOverAAAA(ctx context.Context, dialer *SafeDialer, domain string, start time.Time, withTLS bool,
	try func(ctx context.Context, ip net.IP) (Result, error),
) (Result, error) {
	d := &HTTPDetail{}

	ips, _, _, _, err := dialer.Resolver().LookupAAAA(ctx, domain)
	if err != nil {
		d.Error = err.Error()
		return Result{ //nolint:nilerr // the error is the Result
			Status:  StatusError,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}
	if len(ips) == 0 {
		d.Reason = errNoAAAARecord
		return Result{
			Status:  StatusUnsupported,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	// Try each IP (up to 3).
	maxAttempts := min(len(ips), 3)

	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		ip := ips[i]
		if err := dialer.ValidateIP(ip); err != nil {
			d.Error = errAddrBlocked
			return Result{ //nolint:nilerr // the error is the Result
				Status:  StatusError,
				Detail:  d,
				Latency: time.Since(start),
			}, nil
		}

		result, tryErr := try(ctx, ip)
		if tryErr == nil {
			result.Latency = time.Since(start)
			return result, nil
		}
		lastErr = tryErr

		// Don't retry on context cancellation.
		if ctx.Err() != nil {
			break
		}
	}

	switch {
	case isConnRefused(lastErr):
		d.Error = errConnRefused
		d.ErrorType = ErrTypeConnRefused
		return Result{
			Status:  StatusUnsupported,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	case isTimeout(lastErr):
		d.Error = lastErr.Error()
		d.ErrorType = ErrTypeTimeout
		return Result{
			Status:  StatusError,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	case withTLS && isTLSError(lastErr):
		d.Error = lastErr.Error()
		d.ErrorType = ErrTypeCertificate
		return Result{
			Status:  StatusUnsupported,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	d.Error = lastErr.Error()
	d.ErrorType = ErrTypeUnknown
	return Result{
		Status:  StatusError,
		Detail:  d,
		Latency: time.Since(start),
	}, nil
}
