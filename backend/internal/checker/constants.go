package checker

// Common error messages.
const (
	errConnRefused     = "connection refused"
	errAddrBlocked     = "address in blocked range"
	errNoAAAARecord    = "no AAAA record"
	reasonNoAAAARecord = "no AAAA record on base domain"
	reasonNoMXWithAAAA = "no MX with AAAA record"
)

// NoNSRecordsMessage is the dns_ns_ipv6 error text for a walk-up that
// reached the registrable-domain boundary with only empty answers. It is
// normative evidence, not a message: 03-state-machine.md §4 dead-detection
// branch (a) reads exactly this string as "no delegated zone", and every
// other NS error is treated as resolver trouble (zone assumed present).
const NoNSRecordsMessage = "no NS records found"
