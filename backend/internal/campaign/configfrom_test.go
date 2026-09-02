package campaign

import (
	"os"
	"path/filepath"
	"testing"
)

// TestConfigValidate covers the startup gate: the crawler used to bind these
// defaults unchecked, so a deployment without the checkout (or without git,
// which the distroless runtime image lacks) looked healthy all day and
// failed once a night at the 03:30 tick.
func TestConfigValidate(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		cfg     Config
		noGit   bool
		wantErr bool
	}{
		{"checkout and git present", Config{RepoPath: dir, Pull: true, Push: true}, false, false},
		{"checkout missing", Config{RepoPath: filepath.Join(dir, "absent"), Pull: true}, false, true},
		{"checkout is a file", Config{RepoPath: file, Pull: true}, false, true},
		{"git needed but absent", Config{RepoPath: dir, Pull: true}, true, true},
		{"git absent but not needed", Config{RepoPath: dir}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.noGit {
				t.Setenv("PATH", t.TempDir()) // an empty PATH: no git anywhere
			}
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}
