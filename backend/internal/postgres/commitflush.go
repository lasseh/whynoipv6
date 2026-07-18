package postgres

import (
	"context"
	"fmt"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// CommitUnit is one domain's typed commit write unit (03 §12): the
// lease-fenced domain UPDATE plus its dependent rows, flushed as one
// pgx.Batch in one pgx.Tx. Every statement binds through its sqlc params
// struct — the column↔placeholder order lives with the generated SQL,
// never in a caller.
type CommitUnit struct {
	Domain     db.CommitDomainParams // statement 1 — the fenced UPDATE, carries DomainID + Lease
	Changelog  []db.InsertChangelogParams
	Scan       db.InsertScanParams
	Detail     db.InsertScanDetailParams
	Resources  []string // hosts to ensure/upsert; empty = no link statements
	PruneLinks bool
}

// FlushCommit sends the whole unit as one pgx.Batch in one pgx.Tx (03
// §12.2). leaseLost=true means the fence matched 0 rows and the deferred
// rollback discarded everything; nothing was written.
func FlushCommit(ctx context.Context, pool *pgxpool.Pool, u *CommitUnit) (leaseLost bool, err error) {
	batch := &pgx.Batch{}
	queueParams(batch, db.CommitDomain, u.Domain)
	for _, cl := range u.Changelog {
		queueParams(batch, db.InsertChangelog, cl)
	}
	queueParams(batch, db.InsertScan, u.Scan)
	queueParams(batch, db.InsertScanDetail, u.Detail)
	for _, host := range u.Resources {
		batch.Queue(db.EnsureResourceHost, host)
		batch.Queue(SQLUpsertDomainResource, host, u.Domain.DomainID, u.Domain.Ts)
	}
	if u.PruneLinks {
		batch.Queue(SQLPruneDomainResources, u.Domain.DomainID, u.Domain.Ts)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful Commit

	br := tx.SendBatch(ctx, batch)
	tag, firstErr := br.Exec() // statement 1: the fenced domain UPDATE
	leaseLost = firstErr == nil && tag.RowsAffected() == 0
	for i := 1; i < batch.Len(); i++ { // drain every remaining result
		if _, e := br.Exec(); e != nil && firstErr == nil {
			firstErr = e
		}
	}
	if e := br.Close(); e != nil && firstErr == nil {
		firstErr = e
	}
	if leaseLost {
		return true, nil // deferred Rollback discards EVERYTHING
	}
	if firstErr != nil {
		return false, fmt.Errorf("commit batch: %w", firstErr)
	}
	return false, tx.Commit(ctx)
}

// queueParams queues one sqlc statement with its args taken from the
// generated params struct: sqlc emits the fields in $1..$n placeholder
// order, so declaration order IS the binding (guarded by
// TestCommitStatementBinding against the SQL text).
func queueParams(b *pgx.Batch, sql string, params any) {
	v := reflect.ValueOf(params)
	args := make([]any, v.NumField())
	for i := range args {
		args[i] = v.Field(i).Interface()
	}
	b.Queue(sql, args...)
}

// The two multi-CTE resource statements (the 05-schema.md §10.2 escape
// hatch); the SQL text is 03 §12.3's.
const (
	SQLUpsertDomainResource = `WITH rh AS (
  SELECT id FROM resource_host WHERE host = $1
), ins AS (
  INSERT INTO domain_resource (domain_id, resource_host_id, source, required, first_seen, last_seen)
  SELECT $2, rh.id, 'discovered', TRUE, $3, $3 FROM rh
  ON CONFLICT (domain_id, resource_host_id) DO NOTHING
  RETURNING resource_host_id
), bump AS (
  UPDATE resource_host SET dependent_count = dependent_count + 1
  WHERE id IN (SELECT resource_host_id FROM ins)
)
UPDATE domain_resource SET last_seen = $3
WHERE domain_resource.domain_id = $2
  AND domain_resource.resource_host_id IN (SELECT id FROM rh)
  AND NOT EXISTS (SELECT 1 FROM ins)`

	SQLPruneDomainResources = `WITH del AS (
  DELETE FROM domain_resource
  WHERE domain_id = $1
    AND source = 'discovered'
    AND last_seen < $2::timestamptz - INTERVAL '30 days'
  RETURNING resource_host_id
)
UPDATE resource_host SET dependent_count = dependent_count - 1
WHERE id IN (SELECT resource_host_id FROM del)`
)
