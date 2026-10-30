// Package buildinfo names the commit a binary was built from, so a stale
// deploy shows up in one log line.
package buildinfo

import "runtime/debug"

// Unknown is the revision of a build that recorded none.
const Unknown = "unknown"

// stamped is set with -ldflags -X by the Docker build, whose context has no
// .git for the toolchain to read. Every other build path leaves it empty:
// `go build` in a checkout stamps vcs.revision on its own, and -s -w keeps it.
var stamped string

// Revision returns the commit this binary was built from and whether the
// tree had uncommitted changes. It never fails: a build without a revision
// reports Unknown.
func Revision() (revision string, dirty bool) {
	var settings []debug.BuildSetting
	if info, ok := debug.ReadBuildInfo(); ok {
		settings = info.Settings
	}
	return resolve(stamped, settings)
}

// Attrs is Revision as slog key/value pairs.
func Attrs() []any {
	revision, dirty := Revision()
	return []any{"revision", revision, "dirty", dirty}
}

func resolve(stamped string, settings []debug.BuildSetting) (revision string, dirty bool) {
	if stamped != "" {
		return stamped, false
	}
	revision = Unknown
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	return revision, dirty
}
