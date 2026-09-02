package main

import (
	"strings"
	"testing"

	"github.com/lasseh/whynoipv6/internal/campaign"
)

// TestSyncOutcome pins the exit-code contract from review issue 42: a sync
// that completed *with rejections* is not a clean run. The webhook-triggered
// CI job and systemd OnFailure read the exit code and nothing else, so a
// rejected file or a frozen curated diff has to reach them through it.
func TestSyncOutcome(t *testing.T) {
	for name, tc := range map[string]struct {
		rep      campaign.Report
		wantErr  bool
		contains string
	}{
		"clean run": {
			rep: campaign.Report{WriteBack: "pushed"},
		},
		"nothing to push is clean": {
			rep: campaign.Report{WriteBack: "nothing to push"},
		},
		"rejected file": {
			rep:      campaign.Report{WriteBack: "pushed", RejectedFiles: map[string]string{"nrk.no.yml": "bad label"}},
			wantErr:  true,
			contains: "1 rejected file(s)",
		},
		"rejected host": {
			rep:      campaign.Report{WriteBack: "pushed", RejectedHosts: map[string]string{"x.example": "not a public suffix"}},
			wantErr:  true,
			contains: "1 rejected host(s)",
		},
		"curated frozen": {
			rep:      campaign.Report{WriteBack: "pushed", CuratedFrozen: true},
			wantErr:  true,
			contains: "curated removals frozen",
		},
		"write-back failed": {
			rep:      campaign.Report{WriteBack: "failed: push rejected"},
			wantErr:  true,
			contains: "uuid write-back failed",
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := syncOutcome(&tc.rep)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error: %t", err, tc.wantErr)
			}
			if tc.contains != "" && !strings.Contains(err.Error(), tc.contains) {
				t.Errorf("err = %q, want it to name %q", err, tc.contains)
			}
		})
	}
}
