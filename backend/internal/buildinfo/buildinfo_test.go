package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestResolve(t *testing.T) {
	vcs := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "abc123"},
		{Key: "vcs.modified", Value: "true"},
	}
	tests := []struct {
		name     string
		stamped  string
		settings []debug.BuildSetting
		wantRev  string
		wantDirt bool
	}{
		{name: "docker: the stamped value, no vcs info", stamped: "def456", wantRev: "def456"},
		{name: "go build in a checkout", settings: vcs, wantRev: "abc123", wantDirt: true},
		{
			name:     "a clean checkout",
			settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc123"}, {Key: "vcs.modified", Value: "false"}},
			wantRev:  "abc123",
		},
		// A stamped value names what the builder was told to build, and vcs
		// info is absent in that path anyway, so it never reads as dirty.
		{name: "the stamped value wins over vcs info", stamped: "def456", settings: vcs, wantRev: "def456"},
		{name: "no revision anywhere", wantRev: Unknown},
		{
			name:     "unrelated settings only",
			settings: []debug.BuildSetting{{Key: "GOOS", Value: "linux"}},
			wantRev:  Unknown,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rev, dirty := resolve(tt.stamped, tt.settings)
			if rev != tt.wantRev || dirty != tt.wantDirt {
				t.Errorf("resolve = (%q, %v), want (%q, %v)", rev, dirty, tt.wantRev, tt.wantDirt)
			}
		})
	}
}
