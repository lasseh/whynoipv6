package export

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// Source is the row seam under the exporter — the only place the export
// touches a database. Everything else in this package is a filesystem
// protocol (tiers, digests, SHA256SUMS, the datapackage, retention, the
// manifest, publication), and putting the two database reads behind this
// interface is what lets that protocol be tested without a container.
//
// Two adapters: pgSource in production, and a slice-backed fake in the
// package tests, where row volume and the list ID become test inputs.
type Source interface {
	// Rows streams one tier's exported records in export order. The
	// sequence yields a non-nil error at most once, as its final element;
	// callers stop on the first error.
	Rows(ctx context.Context, rankedOnly bool, maxRank int32) iter.Seq2[Row, error]

	// ListID is the newest successful Tranco import's list ID, or "" when
	// no import has succeeded yet. Only that case degrades attribution; a
	// failed read is an error, because the snapshot it would stamp is
	// immutable once published (07 §5.3: cite the specific list ID).
	ListID(ctx context.Context) (string, error)
}

// pgSource is the production adapter at the Source seam.
type pgSource struct{ pool *pgxpool.Pool }

func (s pgSource) Rows(ctx context.Context, rankedOnly bool, maxRank int32) iter.Seq2[Row, error] {
	return func(yield func(Row, error) bool) {
		rows, err := s.pool.Query(ctx, db.ExportRows, rankedOnly, maxRank)
		if err != nil {
			yield(Row{}, fmt.Errorf("export rows: %w", err))
			return
		}
		defer rows.Close()
		for rows.Next() {
			d, err := pgx.RowToStructByPos[db.ExportRowsRow](rows)
			if err != nil {
				yield(Row{}, fmt.Errorf("export scan: %w", err))
				return
			}
			if !yield(exportRow(&d), nil) {
				return
			}
		}
		if err := rows.Err(); err != nil {
			yield(Row{}, fmt.Errorf("export rows: %w", err))
		}
	}
}

func (s pgSource) ListID(ctx context.Context) (string, error) {
	listID, err := db.New(s.pool).TrancoLatestSuccessListID(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("tranco list id: %w", err)
	}
	return listID, nil
}

// src resolves the seam: an injected Source wins, otherwise the pool is
// adapted. Keeping the fallback here means production wiring still only
// sets Pool.
func (e *Exporter) src() Source {
	if e.Source != nil {
		return e.Source
	}
	return pgSource{pool: e.Pool}
}
