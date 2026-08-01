package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/lasseh/whynoipv6/internal/export"
)

// getDatasets is GET /datasets (07 §5.3): re-reads $DATASETS_DIR/
// manifest.json from disk on every request — the exporter rewrites it
// atomically, so a read never sees a torn file. Missing or unparseable →
// 503 manifest-unavailable, the API's only 503. The dataset files
// themselves are served by nginx, not this process.
func (s *Server) getDatasets(w http.ResponseWriter, r *http.Request) {
	raw, err := os.ReadFile(filepath.Join(s.opts.DatasetsDir, "manifest.json"))
	if err != nil {
		ManifestUnavailable(w, r)
		return
	}
	// Parse only to validate; serve the exporter's bytes verbatim so a
	// schema_version bump never silently drops fields here.
	var m export.Manifest
	if err := json.Unmarshal(raw, &m); err != nil || m.SchemaVersion == 0 {
		ManifestUnavailable(w, r)
		return
	}
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(s.opts.ManifestCacheTTL.Seconds())))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}
