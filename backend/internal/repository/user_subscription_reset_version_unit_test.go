package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestUserSubscriptionResetVersion_MutationsInvalidateOldRound(t *testing.T) {
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	u, err := client.User.Create().SetEmail("reset-version@test.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	g, err := client.Group.Create().SetName("reset-version").Save(ctx)
	require.NoError(t, err)
	now := time.Now().Truncate(time.Microsecond)
	s, err := client.UserSubscription.Create().SetUserID(u.ID).SetGroupID(g.ID).
		SetStartsAt(now).SetExpiresAt(now.Add(30 * 24 * time.Hour)).Save(ctx)
	require.NoError(t, err)
	r := NewUserSubscriptionRepository(client)
	version := func() int64 {
		var v int64
		require.NoError(t, db.QueryRowContext(ctx, "SELECT daily_reset_version FROM user_subscriptions WHERE id = ?", s.ID).Scan(&v))
		return v
	}
	require.EqualValues(t, 0, version())
	require.NoError(t, r.ActivateWindows(ctx, s.ID, now, now))
	require.EqualValues(t, 1, version())
	require.NoError(t, r.ResetDailyUsage(ctx, s.ID, &now, now.Add(24*time.Hour)))
	require.EqualValues(t, 2, version())
	require.NoError(t, r.ResetDailyUsage(ctx, s.ID, &now, now.Add(24*time.Hour)))
	require.EqualValues(t, 2, version(), "stale natural refresh must not advance the round")
	require.NoError(t, r.ResetUsageWindows(ctx, s.ID, true, false, false, now, now))
	require.EqualValues(t, 3, version())
	require.NoError(t, r.ExtendExpiry(ctx, s.ID, now.Add(40*24*time.Hour)))
	require.EqualValues(t, 4, version())
	require.NoError(t, r.UpdateStatus(ctx, s.ID, service.SubscriptionStatusSuspended))
	require.EqualValues(t, 5, version())
}
