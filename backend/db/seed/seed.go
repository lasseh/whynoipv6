// Package seed embeds the checked-in reference data v6ctl loads into the
// database, so seeding needs no files on disk (mirrors db/migrations).
package seed

import _ "embed"

// DNSProviders is the curated ns_host → provider mapping (06-ingest.md §6.11),
// the default input to `v6ctl provider seed`. dns_provider.seed_path overrides
// it with an operator-maintained file.
//
//go:embed dns_provider.yaml
var DNSProviders []byte
