// Package postgres holds the hand-written adapters over the sqlc-generated
// db/ subpackage — the 05-schema.md §10.2 carve-out, bounded to the keyset
// list walks (domains, ASN leaderboard, changelog) and the multi-CTE commit
// statements. Every other query stays sqlc.
package postgres
