package postgres

import "strings"

// likeEscaper makes the LIKE metacharacters literal. PostgreSQL's default
// LIKE escape character is the backslash, so it is escaped first.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// likeSubstring builds the bound `%term%` pattern for the two substring
// searches (07 §3.3 ?q= on hosts, §4.6 ?q= on network names) so that %, _
// and \ in the caller's term match themselves instead of acting as
// wildcards. The pattern is a bind parameter either way; escaping only
// changes what it matches.
func likeSubstring(term string) string {
	return "%" + likeEscaper.Replace(term) + "%"
}
