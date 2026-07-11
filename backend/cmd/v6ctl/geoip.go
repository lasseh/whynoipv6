package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
	"github.com/spf13/cobra"
)

const (
	liteURL      = "https://ipinfo.io/data/ipinfo_lite.mmdb"
	liteFilename = "ipinfo_lite.mmdb"
)

// geoipCmd manages the IPinfo Lite mmdb. It overrides the root's config/DB
// PersistentPreRunE — fetching the database needs no database.
func geoipCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "geoip",
		Short:             "GeoIP database management (IPinfo Lite)",
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
	}

	var token, dir, url string
	update := &cobra.Command{
		Use:   "update",
		Short: "Download the IPinfo Lite mmdb into GEOIP_PATH (dev init + prod timer)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if token == "" {
				token = os.Getenv("IPINFO_TOKEN")
			}
			if token == "" {
				return fmt.Errorf("ipinfo token required (--token or IPINFO_TOKEN)")
			}
			if dir == "" {
				if dir = os.Getenv("GEOIP_PATH"); dir == "" {
					dir = "/var/lib/GeoIP"
				}
			}
			return geoipUpdate(cmd.Context(), url, token, dir)
		},
	}
	update.Flags().StringVar(&token, "token", "", "IPinfo token (default $IPINFO_TOKEN)")
	update.Flags().StringVar(&dir, "dir", "", "destination directory (default $GEOIP_PATH, else /var/lib/GeoIP)")
	update.Flags().StringVar(&url, "url", liteURL, "source URL")
	cmd.AddCommand(update)
	return cmd
}

// geoipUpdate downloads the Lite mmdb to a temp file, verifies it opens as a
// valid database, then atomically renames it into place so the crawler's
// hourly mtime reload (06 §6.8) picks it up without a torn read. The token
// rides an Authorization header, never the URL, so it can't leak into error
// strings.
func geoipUpdate(ctx context.Context, url, token, dir string) error {
	dest := filepath.Join(dir, liteFilename)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: unexpected status %s", resp.Status)
	}

	tmp, err := os.CreateTemp(dir, liteFilename+".*.tmp")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	// CreateTemp is 0600; the crawler reads the volume as a non-root user.
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	if r, err := maxminddb.Open(tmpName); err != nil {
		return fmt.Errorf("verify: %w", err)
	} else {
		_ = r.Close()
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("install: %w", err)
	}
	fmt.Printf("geoip: installed %s\n", dest)
	return nil
}
