//go:build integration

package pgtest

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

// The harness bootstraps itself: Main resolves the server, sets adminDSN and
// builds the template, exactly as it does for every consuming package.
func TestMain(m *testing.M) { os.Exit(Main(m)) }

// TestEnsureTemplateDropsStaleVersions: the template name carries the
// migration version it was built from, so a checkout that adds a migration
// must rebuild rather than clone a schema missing it. Planting a template
// from an older version and running ensureTemplate must clear it.
func TestEnsureTemplateDropsStaleVersions(t *testing.T) {
	ctx := context.Background()
	if err := ensureTemplate(ctx); err != nil { // make sure the server is up
		t.Fatal(err)
	}

	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(ctx) }()

	stale := templatePrefix + "0"
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+stale+" WITH (FORCE)"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+stale); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.WithoutCancel(ctx), "DROP DATABASE IF EXISTS "+stale+" WITH (FORCE)")
	})

	if err := ensureTemplate(ctx); err != nil {
		t.Fatal(err)
	}

	var staleLeft, currentThere bool
	if err := admin.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM pg_database WHERE datname = $1),
		EXISTS (SELECT 1 FROM pg_database WHERE datname = $2)`,
		stale, templateDB()).Scan(&staleLeft, &currentThere); err != nil {
		t.Fatal(err)
	}
	if staleLeft {
		t.Errorf("%s survived; a stale template would be cloned by every test", stale)
	}
	if !currentThere {
		t.Errorf("%s missing after ensureTemplate", templateDB())
	}
}
