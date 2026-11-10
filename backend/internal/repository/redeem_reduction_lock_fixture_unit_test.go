//go:build unit && !integration

package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/stretchr/testify/require"
)

// Run these same assertions against an explicitly configured loopback server
// when Docker is unavailable. The migration fixture creates and removes its own
// database; it never migrates the supplied database or changes global TestMain.
func reductionLockFixtureDatabase(t *testing.T) (*dbent.Client, *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	require.NoError(t, ApplyMigrations(ctx, db))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return client, db
}
