package campaign

import "testing"

// TestGitRemoteShape: the remote reaches git as an argv position.
func TestGitRemoteShape(t *testing.T) {
	for _, ok := range []string{"origin", "upstream", "github/main", "deploy.key-1"} {
		if !gitRemoteRe.MatchString(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "-origin", "--upload-pack=x", "https://x-access-token:T@github.com/o/r.git", "git@github.com:o/r.git"} {
		if gitRemoteRe.MatchString(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}
