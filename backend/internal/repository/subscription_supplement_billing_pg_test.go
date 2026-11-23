//go:build unit

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"

	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func supplementBillingDB(t *testing.T) (*sql.DB, time.Time) {
	t.Helper()
	dsn := os.Getenv("QQ_INVITE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("local QQ_INVITE_TEST_DATABASE_URL required")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", u.Hostname())
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := fmt.Sprintf("supplement_billing_%d", time.Now().UnixNano())
	_, err = admin.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	q.Set("TimeZone", "UTC")
	q.Set("default_transaction_isolation", "repeatable read")
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close(); _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE"); _ = admin.Close() })
	_, err = db.Exec(`CREATE TABLE groups(id BIGINT PRIMARY KEY,deleted_at TIMESTAMPTZ,status TEXT NOT NULL DEFAULT 'active',subscription_type TEXT NOT NULL DEFAULT 'subscription',daily_limit_usd NUMERIC(20,10),weekly_limit_usd NUMERIC(20,10),monthly_limit_usd NUMERIC(20,10));
CREATE TABLE user_subscriptions(id BIGINT PRIMARY KEY,user_id BIGINT NOT NULL,group_id BIGINT NOT NULL REFERENCES groups(id),entitlement_kind TEXT NOT NULL DEFAULT 'standard',starts_at TIMESTAMPTZ NOT NULL,expires_at TIMESTAMPTZ NOT NULL,status TEXT NOT NULL DEFAULT 'active',welfare_cycle_closed BOOLEAN NOT NULL DEFAULT false,deleted_at TIMESTAMPTZ,updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),daily_usage_usd NUMERIC(20,10) NOT NULL DEFAULT 0,weekly_usage_usd NUMERIC(20,10) NOT NULL DEFAULT 0,monthly_usage_usd NUMERIC(20,10) NOT NULL DEFAULT 0,welfare_daily_limit_usd NUMERIC(20,10),welfare_weekly_limit_usd NUMERIC(20,10),welfare_monthly_limit_usd NUMERIC(20,10));
CREATE TABLE payment_orders(id BIGINT PRIMARY KEY);
CREATE TABLE usage_billing_dedup(id BIGSERIAL PRIMARY KEY,request_id TEXT NOT NULL,api_key_id BIGINT NOT NULL,request_fingerprint TEXT NOT NULL,UNIQUE(request_id,api_key_id));
CREATE TABLE usage_billing_dedup_archive(request_id TEXT NOT NULL,api_key_id BIGINT NOT NULL,request_fingerprint TEXT NOT NULL);
INSERT INTO groups(id,daily_limit_usd,weekly_limit_usd,monthly_limit_usd) VALUES(2,10,20,50);`)
	require.NoError(t, err)
	for _, name := range []string{"261_subscription_supplement_balances.sql", "264_paid_subscription_monthly_renewals.sql"} {
		migration, err := os.ReadFile("../../migrations/" + name)
		require.NoError(t, err)
		_, err = db.Exec(string(migration))
		require.NoError(t, err)
	}
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	_, err = db.Exec(`INSERT INTO user_subscriptions(id,user_id,group_id,starts_at,expires_at,daily_usage_usd,weekly_usage_usd,monthly_usage_usd) VALUES(3,1,2,$1,$1::timestamptz+interval '30 days',8,19,40)`, start)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO user_subscription_supplement_balances(subscription_id,term_starts_at,granted_usd) VALUES(3,$1,5)`, start)
	require.NoError(t, err)
	return db, start
}

func TestSubscriptionSupplementBillingSplitsAndDeduplicates(t *testing.T) {
	db, start := supplementBillingDB(t)
	id := int64(3)
	cmd := &service.UsageBillingCommand{RequestID: "split", APIKeyID: 7, UserID: 1, SubscriptionID: &id, SubscriptionTermStartsAt: &start, SubscriptionCost: 3}
	repo := &usageBillingRepository{db: db}
	result, err := repo.Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.True(t, result.Applied)
	var daily, weekly, monthly, used float64
	require.NoError(t, db.QueryRow(`SELECT s.daily_usage_usd,s.weekly_usage_usd,s.monthly_usage_usd,b.used_usd FROM user_subscriptions s JOIN user_subscription_supplement_balances b ON b.subscription_id=s.id WHERE s.id=3`).Scan(&daily, &weekly, &monthly, &used))
	require.Equal(t, 9.0, daily)
	require.Equal(t, 20.0, weekly)
	require.Equal(t, 41.0, monthly)
	require.Equal(t, 2.0, used)
	result, err = repo.Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.False(t, result.Applied)
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM user_subscription_supplement_consumptions`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestSubscriptionSupplementBatchAllocatesAndReleasesHolds(t *testing.T) {
	db, start := supplementBillingDB(t)
	ctx := context.Background()
	repo := &usageBillingRepository{db: db}
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	reservations := &userSubscriptionRepository{client: client}
	id := int64(3)
	first := &service.UsageBillingCommand{RequestID: "batch-first", UserID: 1, APIKeyID: 7, SubscriptionID: &id, SubscriptionTermStartsAt: &start, SubscriptionCost: 3, SubscriptionReservationID: "hold"}
	second := &service.UsageBillingCommand{RequestID: "batch-second", UserID: 1, APIKeyID: 7, SubscriptionID: &id, SubscriptionTermStartsAt: &start, SubscriptionCost: 2}
	hold := &service.SubscriptionSupplementReservation{ID: "hold", SubscriptionID: id, UserID: 1, GroupID: 2, TermStartsAt: start, AmountUSD: 1, TTL: time.Minute}
	require.NoError(t, reservations.ReserveSubscriptionSupplement(ctx, hold))
	out, err := repo.ApplyBatch(ctx, []*service.UsageBillingCommand{first, second})
	require.NoError(t, err)
	require.Equal(t, 1.0, out[0].Result.SubscriptionBaseCost)
	require.Equal(t, 2.0, out[0].Result.SubscriptionSupplementCost)
	require.Equal(t, 0.0, out[1].Result.SubscriptionBaseCost)
	require.Equal(t, 2.0, out[1].Result.SubscriptionSupplementCost)
	// An idempotent retry carries a fresh pre-authorization without a new charge.
	hold.ID = "retry"
	require.NoError(t, reservations.ReserveSubscriptionSupplement(ctx, hold))
	first.SubscriptionReservationID = hold.ID
	out, err = repo.ApplyBatch(ctx, []*service.UsageBillingCommand{first, second})
	require.NoError(t, err)
	require.False(t, out[0].Result.Applied)
	var open, receipts int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM user_subscription_supplement_reservations WHERE released_at IS NULL").Scan(&open))
	require.Zero(t, open)
	require.NoError(t, db.QueryRow("SELECT count(*) FROM user_subscription_supplement_consumptions").Scan(&receipts))
	require.Equal(t, 2, receipts)
}

func TestSubscriptionSupplementBillingLateTermLeavesNewEntitlementUntouched(t *testing.T) {
	db, start := supplementBillingDB(t)
	newStart := start.Add(31 * 24 * time.Hour)
	_, err := db.Exec(`UPDATE user_subscriptions SET starts_at=$1,daily_usage_usd=0,weekly_usage_usd=0,monthly_usage_usd=0 WHERE id=3`, newStart)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO user_subscription_supplement_balances(subscription_id,term_starts_at,granted_usd) VALUES(3,$1,100)`, newStart)
	require.NoError(t, err)
	id := int64(3)
	_, err = (&usageBillingRepository{db: db}).Apply(context.Background(), &service.UsageBillingCommand{RequestID: "late", APIKeyID: 7, UserID: 1, SubscriptionID: &id, SubscriptionTermStartsAt: &start, SubscriptionCost: 3})
	require.NoError(t, err)
	var usage, oldUsed, newUsed float64
	require.NoError(t, db.QueryRow(`SELECT daily_usage_usd FROM user_subscriptions WHERE id=3`).Scan(&usage))
	require.NoError(t, db.QueryRow(`SELECT used_usd FROM user_subscription_supplement_balances WHERE subscription_id=3 AND term_starts_at=$1`, start).Scan(&oldUsed))
	require.NoError(t, db.QueryRow(`SELECT used_usd FROM user_subscription_supplement_balances WHERE subscription_id=3 AND term_starts_at=$1`, newStart).Scan(&newUsed))
	require.Zero(t, usage)
	require.Equal(t, 2.0, oldUsed)
	require.Zero(t, newUsed)
}

func TestPaidMonthlyQuotaAppliesToSupplementReservationAndSettlement(t *testing.T) {
	db, start := supplementBillingDB(t)
	_, err := db.Exec(`UPDATE groups SET daily_limit_usd=NULL,weekly_limit_usd=NULL WHERE id=2;
UPDATE user_subscriptions SET paid_monthly_limit_usd=90,paid_monthly_legacy_expires_at=expires_at,paid_monthly_legacy_limit_usd=50 WHERE id=3`)
	require.NoError(t, err)
	ctx := context.Background()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := &userSubscriptionRepository{client: client}
	hold := &service.SubscriptionSupplementReservation{ID: "paid-term-hold", SubscriptionID: 3, UserID: 1, GroupID: 2, TermStartsAt: start, AmountUSD: 20, TTL: time.Minute}
	require.NoError(t, repo.ReserveSubscriptionSupplement(ctx, hold))

	id := int64(3)
	result, err := (&usageBillingRepository{db: db}).Apply(ctx, &service.UsageBillingCommand{RequestID: "paid-term", APIKeyID: 7, UserID: 1,
		SubscriptionID: &id, SubscriptionTermStartsAt: &start, SubscriptionReservationID: hold.ID, SubscriptionCost: 53})
	require.NoError(t, err)
	require.Equal(t, 50.0, result.SubscriptionBaseCost)
	require.Equal(t, 3.0, result.SubscriptionSupplementCost)
	var monthly float64
	require.NoError(t, db.QueryRow(`SELECT monthly_usage_usd FROM user_subscriptions WHERE id=3`).Scan(&monthly))
	require.Equal(t, 90.0, monthly)
}

func TestPaidMonthlyQuotaFreezesOnlyThePriorTermForLateSettlement(t *testing.T) {
	db, start := supplementBillingDB(t)
	_, err := db.Exec(`UPDATE groups SET daily_limit_usd=NULL,weekly_limit_usd=NULL WHERE id=2;
UPDATE user_subscriptions SET paid_monthly_limit_usd=90,paid_monthly_legacy_expires_at=expires_at,paid_monthly_legacy_limit_usd=50 WHERE id=3`)
	require.NoError(t, err)
	newStart := start.Add(31 * 24 * time.Hour)
	_, err = db.Exec(`UPDATE user_subscriptions SET starts_at=$1,expires_at=$1::timestamptz+interval '30 days',monthly_usage_usd=0,
		paid_monthly_limit_usd=NULL,paid_monthly_legacy_expires_at=NULL,paid_monthly_legacy_limit_usd=NULL WHERE id=3`, newStart)
	require.NoError(t, err)
	id := int64(3)
	result, err := (&usageBillingRepository{db: db}).Apply(context.Background(), &service.UsageBillingCommand{RequestID: "paid-closed-term",
		APIKeyID: 7, UserID: 1, SubscriptionID: &id, SubscriptionTermStartsAt: &start, SubscriptionCost: 20})
	require.NoError(t, err)
	require.Equal(t, 20.0, result.SubscriptionBaseCost)
	require.Zero(t, result.SubscriptionSupplementCost)
	var remaining, monthly float64
	require.NoError(t, db.QueryRow(`SELECT closed_base_remaining_usd FROM user_subscription_supplement_balances WHERE subscription_id=3 AND term_starts_at=$1`, start).Scan(&remaining))
	require.Equal(t, 30.0, remaining)
	require.NoError(t, db.QueryRow(`SELECT monthly_usage_usd FROM user_subscriptions WHERE id=3`).Scan(&monthly))
	require.Zero(t, monthly)
}

func TestSubscriptionSupplementBillingRecordsActualOveruse(t *testing.T) {
	db, start := supplementBillingDB(t)
	id := int64(3)
	result, err := (&usageBillingRepository{db: db}).Apply(context.Background(), &service.UsageBillingCommand{RequestID: "overuse", APIKeyID: 7, UserID: 1, SubscriptionID: &id, SubscriptionTermStartsAt: &start, SubscriptionCost: 9})
	require.NoError(t, err)
	require.Equal(t, 1.0, result.SubscriptionBaseCost)
	require.Equal(t, 8.0, result.SubscriptionSupplementCost)
	var used float64
	require.NoError(t, db.QueryRow(`SELECT used_usd FROM user_subscription_supplement_balances WHERE subscription_id=3`).Scan(&used))
	require.Equal(t, 8.0, used)
}

func TestSubscriptionSupplementBillingSerializesConcurrentRequests(t *testing.T) {
	db, start := supplementBillingDB(t)
	_, err := db.Exec(`CREATE FUNCTION slow_supplement_settlement() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(0.03); RETURN NEW; END; $$;
CREATE TRIGGER slow_supplement_settlement BEFORE UPDATE ON user_subscription_supplement_balances FOR EACH ROW EXECUTE FUNCTION slow_supplement_settlement()`)
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	gate := make(chan struct{})
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-gate
			id := int64(3)
			_, err := (&usageBillingRepository{db: db}).Apply(context.Background(), &service.UsageBillingCommand{RequestID: fmt.Sprintf("parallel-%d", i), APIKeyID: 7, UserID: 1, SubscriptionID: &id, SubscriptionTermStartsAt: &start, SubscriptionCost: 1})
			errs <- err
		}(i)
	}
	close(gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var used, daily float64
	require.NoError(t, db.QueryRow(`SELECT b.used_usd,s.daily_usage_usd FROM user_subscription_supplement_balances b JOIN user_subscriptions s ON s.id=b.subscription_id`).Scan(&used, &daily))
	require.Equal(t, 5.0, used)
	require.Equal(t, 9.0, daily)
}

func TestSubscriptionSupplementBillingReceiptFailureRollsBackEverything(t *testing.T) {
	db, start := supplementBillingDB(t)
	_, err := db.Exec(`CREATE FUNCTION reject_supplement_usage_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'receipt failed'; END; $$;
CREATE TRIGGER reject_supplement_usage_receipt BEFORE INSERT ON user_subscription_supplement_consumptions FOR EACH ROW EXECUTE FUNCTION reject_supplement_usage_receipt()`)
	require.NoError(t, err)
	id := int64(3)
	_, err = (&usageBillingRepository{db: db}).Apply(context.Background(), &service.UsageBillingCommand{RequestID: "rollback", APIKeyID: 7, UserID: 1, SubscriptionID: &id, SubscriptionTermStartsAt: &start, SubscriptionCost: 3})
	require.Error(t, err)
	var used, daily float64
	var dedup int
	require.NoError(t, db.QueryRow(`SELECT b.used_usd,s.daily_usage_usd FROM user_subscription_supplement_balances b JOIN user_subscriptions s ON s.id=b.subscription_id`).Scan(&used, &daily))
	require.Zero(t, used)
	require.Equal(t, 8.0, daily)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_billing_dedup`).Scan(&dedup))
	require.Zero(t, dedup)
}

func TestSubscriptionSupplementBillingPreservesWelfareZeroAndUnlimitedBase(t *testing.T) {
	for _, welfare := range []bool{false, true} {
		t.Run(fmt.Sprint(welfare), func(t *testing.T) {
			db, start := supplementBillingDB(t)
			_, err := db.Exec(`UPDATE groups SET daily_limit_usd=NULL,weekly_limit_usd=0,monthly_limit_usd=0`)
			require.NoError(t, err)
			if welfare {
				_, err = db.Exec(`UPDATE user_subscriptions SET entitlement_kind='welfare',welfare_monthly_limit_usd=0`)
				require.NoError(t, err)
			}
			id := int64(3)
			result, err := (&usageBillingRepository{db: db}).Apply(context.Background(), &service.UsageBillingCommand{RequestID: "quota", APIKeyID: 7, UserID: 1, SubscriptionID: &id, SubscriptionTermStartsAt: &start, SubscriptionCost: 2})
			require.NoError(t, err)
			if welfare {
				require.Equal(t, 2.0, result.SubscriptionSupplementCost)
				require.Zero(t, result.SubscriptionBaseCost)
			} else {
				require.Equal(t, 2.0, result.SubscriptionBaseCost)
				require.Zero(t, result.SubscriptionSupplementCost)
			}
		})
	}
}

func TestSubscriptionSupplementProjectionReadsOnlyCurrentOpenTerm(t *testing.T) {
	db, start := supplementBillingDB(t)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := &userSubscriptionRepository{client: client}
	sub := &service.UserSubscription{ID: 3, StartsAt: start}
	require.NoError(t, repo.LoadSubscriptionSupplement(context.Background(), sub))
	require.Equal(t, 5.0, sub.SupplementGrantedUSD)
	sub.StartsAt = start.Add(time.Second)
	require.NoError(t, repo.LoadSubscriptionSupplement(context.Background(), sub))
	require.Zero(t, sub.SupplementGrantedUSD, "reused subscription ID must not inherit an earlier term")
	sub.StartsAt = start
	_, err := db.Exec(`UPDATE user_subscription_supplement_balances SET closed_at=now() WHERE subscription_id=3`)
	require.NoError(t, err)
	require.NoError(t, repo.LoadSubscriptionSupplement(context.Background(), sub))
	require.Zero(t, sub.SupplementGrantedUSD)
}

func TestSubscriptionSupplementRedisScopeRenewsWithoutResurrection(t *testing.T) {
	mr, cache, _ := newInflightTestEnv(t, false, 60)
	ctx := context.Background()
	scope := "sub:3:term"
	_, err := cache.ReserveInflightScope(ctx, scope, "r1", 2, time.Minute)
	require.NoError(t, err)
	active, err := cache.RenewInflightScope(ctx, scope, "r1", 2*time.Minute)
	require.NoError(t, err)
	require.True(t, active)
	zkey, hkey := billingInflightScopeKeys(scope)
	require.Greater(t, mr.TTL(zkey), time.Minute)
	require.Greater(t, mr.TTL(hkey), time.Minute)
	require.NoError(t, cache.ReleaseInflightScope(ctx, scope, "r1"))
	active, err = cache.RenewInflightScope(ctx, scope, "r1", time.Minute)
	require.NoError(t, err)
	require.False(t, active)
}
