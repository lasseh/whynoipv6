// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.16.0
// source: stats.sql

package db

import (
	"context"
)

const DomainStats = `-- name: DomainStats :one
SELECT
 count(1) filter (WHERE "ts_check" IS NOT NULL) AS "total_sites",
 count(1) filter (WHERE "ts_check" IS NOT NULL AND check_aaaa = TRUE) AS "total_aaaa",
 count(1) filter (WHERE "ts_check" IS NOT NULL AND check_www = TRUE) AS "total_www",
 count(1) filter (WHERE "ts_check" IS NOT NULL AND check_aaaa = TRUE AND check_www = TRUE) AS "total_both",
 count(1) filter (WHERE "ts_check" IS NOT NULL AND check_ns = TRUE) AS "total_ns",
 count(1) filter (WHERE "ts_check" IS NOT NULL AND check_aaaa = TRUE AND check_www = TRUE AND rank < 1000) AS "top_1k",
 count(1) filter (WHERE "ts_check" IS NOT NULL AND check_ns = TRUE AND rank < 1000) AS "top_ns"
FROM domain_view_list
`

type DomainStatsRow struct {
	TotalSites int64
	TotalAaaa  int64
	TotalWww   int64
	TotalBoth  int64
	TotalNs    int64
	Top1k      int64
	TopNs      int64
}

func (q *Queries) DomainStats(ctx context.Context) (DomainStatsRow, error) {
	row := q.db.QueryRow(ctx, DomainStats)
	var i DomainStatsRow
	err := row.Scan(
		&i.TotalSites,
		&i.TotalAaaa,
		&i.TotalWww,
		&i.TotalBoth,
		&i.TotalNs,
		&i.Top1k,
		&i.TopNs,
	)
	return i, err
}