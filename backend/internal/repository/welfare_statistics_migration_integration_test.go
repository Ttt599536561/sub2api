//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestWelfareStatisticsMigrationRecoversInvalidConcurrentIndex(t *testing.T) {
	// The migration runner checks public indexes, so use a separate database
	// instead of changing the shared harness schema or its migration history.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	container, err := tcpostgres.Run(ctx, selectDockerImage(ctx, postgresImageTag),
		tcpostgres.WithDatabase("welfare_statistics_index_recovery"),
		tcpostgres.WithUsername("postgres"), tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		require.NoError(t, container.Terminate(cleanupCtx))
	})
	dsn, err := container.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, ApplyMigrations(ctx, db))

	const filename = "242_welfare_statistics_business_date_idx_notx.sql"
	const indexName = "welfare_ledger_reward_business_date_idx"
	content, err := migrations.FS.ReadFile(filename)
	require.NoError(t, err)
	sum := sha256.Sum256([]byte(strings.TrimSpace(string(content))))
	wantChecksum := hex.EncodeToString(sum[:])

	_, err = db.ExecContext(ctx, "DROP INDEX CONCURRENTLY public."+indexName)
	require.NoError(t, err)
	result, err := db.ExecContext(ctx, "DELETE FROM schema_migrations WHERE filename = $1", filename)
	require.NoError(t, err)
	removed, err := result.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 1, removed)

	_, err = db.ExecContext(ctx, `
CREATE TABLE welfare_statistics_failed_index_fixture (value INTEGER NOT NULL);
INSERT INTO welfare_statistics_failed_index_fixture (value) VALUES (1), (1);
`)
	require.NoError(t, err)
	// A real failed concurrent build leaves the index in pg_index with
	// indisvalid=false. Do not synthesize that state by editing pg_catalog.
	_, err = db.ExecContext(ctx, "CREATE UNIQUE INDEX CONCURRENTLY "+indexName+
		" ON welfare_statistics_failed_index_fixture (value)")
	require.Error(t, err)
	var buildError *pq.Error
	require.True(t, errors.As(err, &buildError))
	require.Equal(t, pq.ErrorCode("23505"), buildError.Code)

	type indexState struct {
		oid        int64
		valid      bool
		unique     bool
		tableName  string
		definition string
	}
	readIndex := func() indexState {
		t.Helper()
		var state indexState
		require.NoError(t, db.QueryRowContext(ctx, `
SELECT idx.oid::bigint, i.indisvalid, i.indisunique, tbl.relname, pg_get_indexdef(idx.oid)
FROM pg_class idx
JOIN pg_namespace ns ON ns.oid = idx.relnamespace
JOIN pg_index i ON i.indexrelid = idx.oid
JOIN pg_class tbl ON tbl.oid = i.indrelid
WHERE ns.nspname = 'public' AND idx.relname = $1
`, indexName).Scan(&state.oid, &state.valid, &state.unique, &state.tableName, &state.definition))
		return state
	}
	failed := readIndex()
	require.False(t, failed.valid, "failed CONCURRENTLY build must leave a real invalid index")
	require.True(t, failed.unique)
	require.Equal(t, "welfare_statistics_failed_index_fixture", failed.tableName)
	t.Logf("failed concurrent build left index oid=%d indisvalid=%t", failed.oid, failed.valid)

	require.NoError(t, ApplyMigrations(ctx, db))
	recovered := readIndex()
	require.True(t, recovered.valid, "retry must replace the invalid index instead of recording a skipped CREATE")
	require.NotEqual(t, failed.oid, recovered.oid, "invalid index must be dropped and rebuilt")
	require.False(t, recovered.unique)
	require.Equal(t, "welfare_ledger", recovered.tableName)
	require.Contains(t, recovered.definition, "(business_date, user_id) INCLUDE (type, amount_cents)")
	for _, rewardType := range []string{"'daily'", "'streak'", "'draw'"} {
		require.Contains(t, recovered.definition, rewardType)
	}

	var checksum string
	var appliedAt time.Time
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT checksum, applied_at FROM schema_migrations WHERE filename = $1", filename).
		Scan(&checksum, &appliedAt))
	require.Equal(t, wantChecksum, checksum)

	require.NoError(t, ApplyMigrations(ctx, db), "a subsequent startup must be idempotent")
	require.Equal(t, recovered, readIndex(), "valid index must be retained on subsequent startup")
	var recordCount int
	var repeatedChecksum string
	var repeatedAppliedAt time.Time
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT COUNT(*), MIN(checksum), MIN(applied_at) FROM schema_migrations WHERE filename = $1
`, filename).Scan(&recordCount, &repeatedChecksum, &repeatedAppliedAt))
	require.Equal(t, 1, recordCount)
	require.Equal(t, wantChecksum, repeatedChecksum)
	require.Equal(t, appliedAt, repeatedAppliedAt, "idempotent retry must preserve the migration record")
	t.Logf("recovered index oid=%d indisvalid=%t; checksum=%s; repeat retained index and record",
		recovered.oid, recovered.valid, checksum)
}
