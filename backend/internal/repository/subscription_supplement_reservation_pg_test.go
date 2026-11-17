//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionSupplementDurableHoldsSurviveRedisLossAndSettleAtomically(t *testing.T) {
	db, start := supplementBillingDB(t)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo, ok := any(&userSubscriptionRepository{client: client}).(service.SubscriptionSupplementReservationRepository)
	require.True(t, ok, "production repository must provide authoritative durable holds")
	hold := &service.SubscriptionSupplementReservation{ID: "hold-first", SubscriptionID: 3, UserID: 1, GroupID: 2, TermStartsAt: start, AmountUSD: 4, TTL: time.Minute}
	require.NoError(t, repo.ReserveSubscriptionSupplement(context.Background(), hold))
	// No Redis state is needed for this authority: losing all cache keys cannot
	// make the existing database hold disappear from the next budget decision.
	second := *hold
	second.ID = "hold-second"
	second.AmountUSD = 3
	require.Error(t, repo.ReserveSubscriptionSupplement(context.Background(), &second))
	id := int64(3)
	cmd := &service.UsageBillingCommand{RequestID: "held-charge", APIKeyID: 7, UserID: 1, SubscriptionID: &id, SubscriptionTermStartsAt: &start, SubscriptionReservationID: hold.ID, SubscriptionCost: 3}
	_, err := (&usageBillingRepository{db: db}).Apply(context.Background(), cmd)
	require.NoError(t, err)
	var released bool
	require.NoError(t, db.QueryRow(`SELECT released_at IS NOT NULL FROM user_subscription_supplement_reservations WHERE id=$1`, hold.ID).Scan(&released))
	require.True(t, released)
	require.NoError(t, repo.ReserveSubscriptionSupplement(context.Background(), &second), "committed charge replaces its hold atomically")
}

func TestSubscriptionSupplementDurableHoldExpiryAndTaskTransfer(t *testing.T) {
	db, start := supplementBillingDB(t)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo, ok := any(&userSubscriptionRepository{client: client}).(service.SubscriptionSupplementReservationRepository)
	require.True(t, ok)
	hold := &service.SubscriptionSupplementReservation{ID: "hold-expiry", SubscriptionID: 3, UserID: 1, GroupID: 2, TermStartsAt: start, AmountUSD: 6, TTL: time.Minute}
	require.NoError(t, repo.ReserveSubscriptionSupplement(context.Background(), hold))
	require.NoError(t, repo.TransferSubscriptionSupplementReservation(context.Background(), hold.ID, "grok:task-1", 24*time.Hour))
	var task string
	var longTTL bool
	require.NoError(t, db.QueryRow(`SELECT external_task_key,expires_at>now()+interval '23 hours' FROM user_subscription_supplement_reservations WHERE id=$1`, hold.ID).Scan(&task, &longTTL))
	require.Equal(t, "grok:task-1", task)
	require.True(t, longTTL)
	_, err := db.Exec(`UPDATE user_subscription_supplement_reservations SET expires_at=now()-interval '1 second' WHERE id=$1`, hold.ID)
	require.NoError(t, err)
	alive, err := repo.RenewSubscriptionSupplementReservation(context.Background(), hold.ID, time.Minute)
	require.NoError(t, err)
	require.False(t, alive, "expired orphan holds must not be revived")
	second := *hold
	second.ID = "recovered"
	require.NoError(t, repo.ReserveSubscriptionSupplement(context.Background(), &second))
	require.NoError(t, repo.ReleaseSubscriptionSupplementReservation(context.Background(), second.ID))
	require.NoError(t, repo.ReleaseSubscriptionSupplementReservation(context.Background(), second.ID))
}

func TestSubscriptionSupplementDurableHoldZeroCostAndDuplicateSettle(t *testing.T) {
	db, start := supplementBillingDB(t)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := &userSubscriptionRepository{client: client}
	hold := &service.SubscriptionSupplementReservation{ID: "zero-hold", SubscriptionID: 3, UserID: 1, GroupID: 2, TermStartsAt: start, AmountUSD: 4, TTL: time.Minute}
	require.NoError(t, repo.ReserveSubscriptionSupplement(context.Background(), hold))
	require.NoError(t, repo.TransferSubscriptionSupplementReservation(context.Background(), hold.ID, "free-video", 24*time.Hour))
	id := int64(3)
	cmd := &service.UsageBillingCommand{RequestID: "zero-cost", APIKeyID: 7, UserID: 1, SubscriptionID: &id, SubscriptionTermStartsAt: &start, SubscriptionReservationID: hold.ID}
	billing := &usageBillingRepository{db: db}
	_, err := billing.Apply(context.Background(), cmd)
	require.NoError(t, err)
	var released bool
	require.NoError(t, db.QueryRow(`SELECT released_at IS NOT NULL FROM user_subscription_supplement_reservations WHERE id=$1`, hold.ID).Scan(&released))
	require.True(t, released)
	hold.ID = "duplicate-hold"
	require.NoError(t, repo.ReserveSubscriptionSupplement(context.Background(), hold))
	cmd.SubscriptionReservationID = hold.ID
	result, err := billing.Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.NoError(t, db.QueryRow(`SELECT released_at IS NOT NULL FROM user_subscription_supplement_reservations WHERE id=$1`, hold.ID).Scan(&released))
	require.True(t, released, "deduplicated retries release their new hold without charging twice")
}
