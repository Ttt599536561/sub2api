//go:build integration

package service_test

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
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type welfareRedemptionFixture struct {
	ctx    context.Context
	db     *sql.DB
	client *dbent.Client
	userID int64
	group  *dbent.Group
	codes  service.RedeemCodeRepository
	subs   *service.SubscriptionService
}

func newWelfareRedemptionFixture(t *testing.T) *welfareRedemptionFixture {
	t.Helper()
	dsn := os.Getenv("QQ_INVITE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QQ_INVITE_TEST_DATABASE_URL required")
	}
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", parsed.Hostname(), "only dedicated loopback test databases are allowed")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := fmt.Sprintf("welfare_grants_%d", time.Now().UnixNano())
	_, err = admin.ExecContext(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("search_path", schema)
	// The service must explicitly choose Read Committed before its user lock.
	query.Set("default_transaction_isolation", "repeatable read")
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	db.SetMaxOpenConns(6)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close(); _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE"); _ = admin.Close() })
	require.NoError(t, client.Schema.Create(ctx))
	// These raw ledger tables are outside Ent. Include their identity/receipt
	// constraints so redemption uses real SQL, row locks and rollback behavior.
	_, err = db.ExecContext(ctx, `CREATE TABLE qq_invite_code_pool (
 id BIGSERIAL PRIMARY KEY,redeem_code_id BIGINT NOT NULL UNIQUE REFERENCES redeem_codes(id),record_id BIGINT UNIQUE);
CREATE TABLE qq_invite_rewards (
 id BIGSERIAL PRIMARY KEY,record_id BIGINT NOT NULL UNIQUE,pool_id BIGINT NOT NULL UNIQUE REFERENCES qq_invite_code_pool(id));
CREATE TABLE user_welfare_subscription_grants (
 redeem_code_id BIGINT PRIMARY KEY REFERENCES redeem_codes(id),
 subscription_id BIGINT NOT NULL REFERENCES user_subscriptions(id),
 daily_limit_usd NUMERIC(20,10),weekly_limit_usd NUMERIC(20,10),monthly_limit_usd NUMERIC(20,10),
 granted_at TIMESTAMPTZ NOT NULL);`)
	require.NoError(t, err)
	u, err := client.User.Create().SetEmail("welfare@example.test").SetPasswordHash("test-only").Save(ctx)
	require.NoError(t, err)
	// Zero is a valid unlimited optional window and must not reject the card.
	g, err := client.Group.Create().SetName("monthly").SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeSubscription).SetDailyLimitUsd(0).SetMonthlyLimitUsd(100).Save(ctx)
	require.NoError(t, err)
	subs := service.NewSubscriptionService(repository.NewGroupRepository(client, db), repository.NewUserSubscriptionRepository(client), nil, client, nil)
	t.Cleanup(subs.Stop)
	return &welfareRedemptionFixture{ctx: ctx, db: db, client: client, userID: u.ID, group: g,
		codes: repository.NewRedeemCodeRepository(client), subs: subs}
}

func (f *welfareRedemptionFixture) reward(t *testing.T, text string) *service.RedeemCode {
	t.Helper()
	code := &service.RedeemCode{Code: text, Type: service.RedeemTypeSubscription, Status: service.StatusUnused,
		GroupID: &f.group.ID, ValidityDays: 30}
	require.NoError(t, f.codes.Create(f.ctx, code))
	_, err := f.db.ExecContext(f.ctx, `WITH p AS (
 INSERT INTO qq_invite_code_pool(redeem_code_id,record_id) VALUES($1,$1) RETURNING id,record_id
) INSERT INTO qq_invite_rewards(pool_id,record_id) SELECT id,record_id FROM p`, code.ID)
	require.NoError(t, err)
	return code
}

func (f *welfareRedemptionFixture) redeemer(codes service.RedeemCodeRepository) *service.RedeemService {
	return service.NewRedeemService(codes, repository.NewUserRepository(f.client, f.db), f.subs, nil, nil, f.client, nil, nil)
}

func TestConcurrentWelfareRedemptionsStackQuotaAndPreservePaidTerm(t *testing.T) {
	f := newWelfareRedemptionFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	paid, err := f.client.UserSubscription.Create().SetUserID(f.userID).SetGroupID(f.group.ID).
		SetStartsAt(now).SetExpiresAt(now.AddDate(0, 0, 60)).SetDailyUsageUsd(3).
		SetWeeklyUsageUsd(4).SetMonthlyUsageUsd(5).Save(f.ctx)
	require.NoError(t, err)
	codes := []*service.RedeemCode{f.reward(t, "WELFARE-CONCURRENT-A"), f.reward(t, "WELFARE-CONCURRENT-B")}
	barrier := &redemptionUseBarrierRepo{RedeemCodeRepository: f.codes, arrived: make(chan struct{}, 2), release: make(chan struct{})}
	svc := f.redeemer(barrier)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, code := range codes {
		wg.Go(func() { _, err := svc.Redeem(f.ctx, f.userID, code.Code); results <- err })
	}
	for range 2 {
		select {
		case <-barrier.arrived:
		case <-f.ctx.Done():
			t.Fatal("welfare redemption barrier timed out")
		}
	}
	close(barrier.release)
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	welfare, err := f.client.UserSubscription.Query().Where(usersubscription.UserIDEQ(f.userID),
		usersubscription.EntitlementKindEQ(service.SubscriptionEntitlementWelfare)).Only(f.ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, welfare.WelfareGrantCount)
	require.NotNil(t, welfare.WelfareMonthlyLimitUsd)
	require.Equal(t, 200.0, *welfare.WelfareMonthlyLimitUsd)
	require.Nil(t, welfare.WelfareDailyLimitUsd)
	require.WithinDuration(t, welfare.StartsAt.AddDate(0, 0, 30), welfare.ExpiresAt, time.Microsecond)
	unchanged, err := f.client.UserSubscription.Get(f.ctx, paid.ID)
	require.NoError(t, err)
	require.Equal(t, paid.ExpiresAt, unchanged.ExpiresAt)
	require.Equal(t, paid.StartsAt, unchanged.StartsAt)
	require.Equal(t, paid.DailyUsageUsd, unchanged.DailyUsageUsd)
	require.Equal(t, paid.WeeklyUsageUsd, unchanged.WeeklyUsageUsd)
	require.Equal(t, paid.MonthlyUsageUsd, unchanged.MonthlyUsageUsd)
	var receipts int
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM user_welfare_subscription_grants WHERE subscription_id=$1`, welfare.ID).Scan(&receipts))
	require.Equal(t, 2, receipts)
	_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, codes[0].Code)
	require.ErrorIs(t, err, service.ErrRedeemCodeUsed)
	replayed, err := f.client.UserSubscription.Get(f.ctx, welfare.ID)
	require.NoError(t, err)
	require.Equal(t, welfare.WelfareGrantCount, replayed.WelfareGrantCount)
	require.Equal(t, welfare.ExpiresAt, replayed.ExpiresAt)
}

func TestWelfareReceiptFailureRollsBackCodeAndQuota(t *testing.T) {
	f := newWelfareRedemptionFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	welfare, err := f.client.UserSubscription.Create().SetUserID(f.userID).SetGroupID(f.group.ID).
		SetEntitlementKind(service.SubscriptionEntitlementWelfare).SetWelfareGrantCount(1).SetWelfareMonthlyLimitUsd(100).
		SetStartsAt(now.AddDate(0, 0, -2)).SetExpiresAt(now.AddDate(0, 0, 28)).SetMonthlyUsageUsd(35).Save(f.ctx)
	require.NoError(t, err)
	code := f.reward(t, "WELFARE-RECEIPT-FAIL")
	_, err = f.db.ExecContext(f.ctx, `CREATE FUNCTION reject_welfare_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'receipt unavailable'; END $$;
CREATE TRIGGER reject_welfare_receipt BEFORE INSERT ON user_welfare_subscription_grants FOR EACH ROW EXECUTE FUNCTION reject_welfare_receipt();`)
	require.NoError(t, err)
	_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, code.Code)
	require.ErrorContains(t, err, "receipt unavailable")
	got, err := f.client.UserSubscription.Get(f.ctx, welfare.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, got.WelfareGrantCount)
	require.Equal(t, 100.0, *got.WelfareMonthlyLimitUsd)
	require.Equal(t, 35.0, got.MonthlyUsageUsd)
	require.Equal(t, welfare.ExpiresAt, got.ExpiresAt)
	unused, err := f.codes.GetByID(f.ctx, code.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusUnused, unused.Status)
	require.Nil(t, unused.UsedBy)
	var receipts int
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM user_welfare_subscription_grants`).Scan(&receipts))
	require.Zero(t, receipts)
}
