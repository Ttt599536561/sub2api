//go:build unit

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestSubscriptionSupplementDurableReviewScopeAndConcurrentAdmission(t *testing.T) {
	db, start := supplementBillingDB(t)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := &userSubscriptionRepository{client: client}
	base := service.SubscriptionSupplementReservation{ID: "scope", SubscriptionID: 3, UserID: 1, GroupID: 2, TermStartsAt: start, AmountUSD: 2, TTL: time.Minute}
	for _, kind := range []string{"user", "group", "term"} {
		wrong := base
		switch kind {
		case "user":
			wrong.UserID = 99
		case "group":
			wrong.GroupID = 99
		case "term":
			wrong.TermStartsAt = start.Add(time.Hour)
		}
		require.Error(t, repo.ReserveSubscriptionSupplement(context.Background(), &wrong), kind)
	}
	var wg sync.WaitGroup
	gate := make(chan struct{})
	outcomes := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-gate
			h := base
			h.ID = fmt.Sprintf("parallel-%d", i)
			outcomes <- repo.ReserveSubscriptionSupplement(context.Background(), &h)
		}(i)
	}
	close(gate)
	wg.Wait()
	close(outcomes)
	allowed := 0
	for err := range outcomes {
		if err == nil {
			allowed++
		}
	}
	require.Equal(t, 3, allowed, "one base dollar plus five supplement dollars admit exactly three $2 holds")
	var total float64
	require.NoError(t, db.QueryRow(`SELECT sum(amount_usd) FROM user_subscription_supplement_reservations WHERE released_at IS NULL`).Scan(&total))
	require.Equal(t, 6.0, total)
}
func TestSubscriptionSupplementDurableReviewSettlementRollbackAndDedup(t *testing.T) {
	db, start := supplementBillingDB(t)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	holds := &userSubscriptionRepository{client: client}
	hold := service.SubscriptionSupplementReservation{ID: "first", SubscriptionID: 3, UserID: 1, GroupID: 2, TermStartsAt: start, AmountUSD: 4, TTL: time.Minute}
	require.NoError(t, holds.ReserveSubscriptionSupplement(context.Background(), &hold))
	_, err := db.Exec(`CREATE FUNCTION reject_review_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'receipt blocked'; END; $$; CREATE TRIGGER reject_review_receipt BEFORE INSERT ON user_subscription_supplement_consumptions FOR EACH ROW EXECUTE FUNCTION reject_review_receipt()`)
	require.NoError(t, err)
	id := int64(3)
	cmd := &service.UsageBillingCommand{RequestID: "review-atomic", APIKeyID: 7, UserID: 1, SubscriptionID: &id, SubscriptionTermStartsAt: &start, SubscriptionReservationID: hold.ID, SubscriptionCost: 3}
	billing := &usageBillingRepository{db: db}
	_, err = billing.Apply(context.Background(), cmd)
	require.Error(t, err)
	var released bool
	require.NoError(t, db.QueryRow(`SELECT released_at IS NOT NULL FROM user_subscription_supplement_reservations WHERE id=$1`, hold.ID).Scan(&released))
	require.False(t, released, "failed settlement cannot release the durable hold")
	_, err = db.Exec(`DROP TRIGGER reject_review_receipt ON user_subscription_supplement_consumptions`)
	require.NoError(t, err)
	result, err := billing.Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.True(t, result.Applied)
	second := hold
	second.ID = "duplicate"
	second.AmountUSD = 3
	require.NoError(t, holds.ReserveSubscriptionSupplement(context.Background(), &second))
	cmd.SubscriptionReservationID = second.ID
	result, err = billing.Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.NoError(t, db.QueryRow(`SELECT released_at IS NOT NULL FROM user_subscription_supplement_reservations WHERE id=$1`, second.ID).Scan(&released))
	require.True(t, released, "duplicate charge must clear its redundant hold without another debit")
	var used float64
	require.NoError(t, db.QueryRow(`SELECT used_usd FROM user_subscription_supplement_balances WHERE subscription_id=3 AND term_starts_at=$1`, start).Scan(&used))
	require.Equal(t, 2.0, used)
}
func TestSubscriptionSupplementDurableReviewSettlementCannotReleaseAnotherScope(t *testing.T) {
	db, start := supplementBillingDB(t)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	holds := &userSubscriptionRepository{client: client}
	hold := service.SubscriptionSupplementReservation{ID: "protected-hold", SubscriptionID: 3, UserID: 1, GroupID: 2, TermStartsAt: start, AmountUSD: 4, TTL: time.Minute}
	require.NoError(t, holds.ReserveSubscriptionSupplement(context.Background(), &hold))
	billing := &usageBillingRepository{db: db}
	for _, kind := range []string{"user", "subscription", "term"} {
		subID, uid, term := int64(3), int64(1), start
		switch kind {
		case "user":
			uid = 99
		case "subscription":
			subID = 99
		case "term":
			term = start.Add(time.Hour)
		}
		cmd := &service.UsageBillingCommand{RequestID: "review-other-" + kind, APIKeyID: 7, UserID: uid, SubscriptionID: &subID, SubscriptionTermStartsAt: &term, SubscriptionReservationID: hold.ID}
		_, err := billing.Apply(context.Background(), cmd)
		require.NoError(t, err)
		var released bool
		require.NoError(t, db.QueryRow(`SELECT released_at IS NOT NULL FROM user_subscription_supplement_reservations WHERE id=$1`, hold.ID).Scan(&released))
		require.False(t, released, "settlement identity "+kind+" must match before release")
	}
	subID := int64(3)
	cmd := &service.UsageBillingCommand{RequestID: "review-zero-success", APIKeyID: 7, UserID: 1, SubscriptionID: &subID, SubscriptionTermStartsAt: &start, SubscriptionReservationID: hold.ID}
	_, err := billing.Apply(context.Background(), cmd)
	require.NoError(t, err)
	var released bool
	require.NoError(t, db.QueryRow(`SELECT released_at IS NOT NULL FROM user_subscription_supplement_reservations WHERE id=$1`, hold.ID).Scan(&released))
	require.True(t, released, "successful zero-cost settlement still releases its own hold")
}
