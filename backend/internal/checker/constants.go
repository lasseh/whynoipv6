package checker

// Common error messages.
const (
	errConnRefused     = "connection refused"
	errAddrBlocked     = "address in blocked range"
	errNoAAAARecord    = "no AAAA record"
	reasonNoAAAARecord = "no AAAA record on base domain"
	reasonNoMXWithAAAA = "no MX with AAAA record"
)
