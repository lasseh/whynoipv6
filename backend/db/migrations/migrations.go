// Package migrations embeds the golang-migrate SQL files so v6ctl can
// run them via the iofs source driver without shipping a directory.
package migrations

import "embed"

//go:embed *.sql
var Files embed.FS
