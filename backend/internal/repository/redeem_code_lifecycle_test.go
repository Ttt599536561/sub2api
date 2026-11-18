package repository

import (
	"context"
	"database/sql"
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

func TestRedeemCodeLifecycleProtectsConsumedHistory(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	repo := NewRedeemCodeRepository(client)
	u, err := client.User.Create().SetEmail("redeem-lifecycle@example.test").SetPasswordHash("test-only").Save(ctx)
	require.NoError(t, err)

	usedAt := time.Now().UTC().Add(-time.Hour)
	consumed := &service.RedeemCode{Code: "HISTORICAL", Type: service.RedeemTypeBalance, Status: service.StatusExpired,
		Value: 1, UsedBy: &u.ID, UsedAt: &usedAt}
	ordinary := &service.RedeemCode{Code: "ORDINARY", Type: service.RedeemTypeBalance, Status: service.StatusUnused,
		Value: 1}
	require.NoError(t, repo.Create(ctx, consumed))
	require.NoError(t, repo.Create(ctx, ordinary))

	disabled := service.StatusDisabled
	_, err = repo.BatchUpdate(ctx, []int64{ordinary.ID, consumed.ID}, service.RedeemCodeBatchUpdateFields{Status: &disabled})
	require.ErrorIs(t, err, service.ErrRedeemCodeUsed)
	got, err := repo.GetByID(ctx, ordinary.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusUnused, got.Status, "a protected row must leave the rest of the batch intact")

	require.ErrorIs(t, repo.Delete(ctx, consumed.ID), service.ErrRedeemCodeDeleteUsed)
	got, err = repo.GetByID(ctx, consumed.ID)
	require.NoError(t, err)
	require.Equal(t, &u.ID, got.UsedBy)
	require.NoError(t, repo.Delete(ctx, ordinary.ID))
}

func TestRedeemCodeClaimChecksCurrentExpiry(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	repo := NewRedeemCodeRepository(client)
	u, err := client.User.Create().SetEmail("redeem-expiry@example.test").SetPasswordHash("test-only").Save(ctx)
	require.NoError(t, err)
	expiresAt := time.Now().UTC().Add(-time.Hour)
	expired := &service.RedeemCode{Code: "PAST-EXPIRY", Type: service.RedeemTypeBalance, Status: service.StatusUnused,
		Value: 1, ExpiresAt: &expiresAt}
	require.NoError(t, repo.Create(ctx, expired))

	require.ErrorIs(t, repo.Use(ctx, expired.ID, u.ID), service.ErrRedeemCodeExpired)
	got, err := repo.GetByID(ctx, expired.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusUnused, got.Status)
	require.Nil(t, got.UsedAt)
}
