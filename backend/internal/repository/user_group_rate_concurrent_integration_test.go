//go:build integration

package repository

import (
	"context"
	"database/sql"
	"net/url"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestConcurrentGroupRateReplacements(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := testEntClient(t)
	group := mustCreateGroup(t, client, &service.Group{Name: t.Name()})
	// Deployments may configure repeatable read as the connection default.
	dsn, err := url.Parse(integrationDSN)
	require.NoError(t, err)
	options := dsn.Query()
	options.Set("default_transaction_isolation", "repeatable read")
	dsn.RawQuery = options.Encode()
	db, err := sql.Open("postgres", dsn.String())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := NewUserGroupRateRepository(db)
	users := []int64{
		mustCreateUser(t, client, &service.User{Email: t.Name() + "-a@example.com"}).ID,
		mustCreateUser(t, client, &service.User{Email: t.Name() + "-b@example.com"}).ID,
	}
	t.Cleanup(func() {
		for _, id := range users {
			_, err := integrationDB.Exec("DELETE FROM users WHERE id = $1", id)
			require.NoError(t, err)
		}
		_, err := integrationDB.Exec("DELETE FROM groups WHERE id = $1", group.ID)
		require.NoError(t, err)
	})
	// Hold the parent but leave the override list empty. Both writers must wait
	// before scanning that list; child-row locks alone cannot serialize them.
	gate, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = gate.Rollback() }()
	var gatePID int
	require.NoError(t, gate.QueryRowContext(ctx,
		"SELECT pg_backend_pid() FROM groups WHERE id = $1 FOR NO KEY UPDATE", group.ID).Scan(&gatePID))
	done := make(chan error, len(users))
	for _, userID := range users {
		go func() {
			done <- repo.SyncGroupRateMultipliers(ctx, group.ID, []service.GroupRateMultiplierInput{{UserID: userID, RateMultiplier: 2}})
		}()
	}
	require.Eventually(t, func() bool {
		var waiting int
		// A waiter may queue behind the first waiter as well as the lock owner.
		err := integrationDB.QueryRowContext(ctx, `WITH RECURSIVE waiters(pid) AS (
			SELECT $1::int UNION
			SELECT s.pid FROM pg_stat_activity s JOIN waiters w ON w.pid = ANY(pg_blocking_pids(s.pid))
		) SELECT COUNT(*) FROM waiters WHERE pid <> $1`, gatePID).Scan(&waiting)
		return err == nil && waiting == 2
	}, 5*time.Second, 10*time.Millisecond, "both whole-list replacements should wait for the group lock")
	require.NoError(t, gate.Commit())
	for range users {
		require.NoError(t, <-done)
	}
	entries, err := repo.GetByGroupID(ctx, group.ID)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Contains(t, users, entries[0].UserID)

	rpm := 9
	userID := entries[0].UserID
	require.NoError(t, repo.SyncGroupRPMOverrides(ctx, group.ID, []service.GroupRPMOverrideInput{{UserID: userID, RPMOverride: &rpm}}))
	require.NoError(t, repo.ClearGroupRPMOverrides(ctx, group.ID))
	entries, err = repo.GetByGroupID(ctx, group.ID)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.NotNil(t, entries[0].RateMultiplier)
	require.Equal(t, 2.0, *entries[0].RateMultiplier)
	require.Nil(t, entries[0].RPMOverride)

	require.NoError(t, repo.SyncGroupRPMOverrides(ctx, group.ID, []service.GroupRPMOverrideInput{{UserID: userID, RPMOverride: &rpm}}))
	require.NoError(t, repo.SyncGroupRateMultipliers(ctx, group.ID, nil))
	entries, err = repo.GetByGroupID(ctx, group.ID)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Nil(t, entries[0].RateMultiplier)
	require.Equal(t, &rpm, entries[0].RPMOverride)
	require.NoError(t, repo.ClearGroupRPMOverrides(ctx, group.ID))
	entries, err = repo.GetByGroupID(ctx, group.ID)
	require.NoError(t, err)
	require.Empty(t, entries)
}
