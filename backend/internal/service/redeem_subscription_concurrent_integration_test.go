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
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type redemptionUseBarrierRepo struct {
	service.RedeemCodeRepository
	arrived chan struct{}
	release chan struct{}
}

func (r *redemptionUseBarrierRepo) Use(ctx context.Context, id, userID int64) error {
	if err := r.RedeemCodeRepository.Use(ctx, id, userID); err != nil {
		return err
	}
	r.arrived <- struct{}{}
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestConcurrentFirstSubscriptionRedemptionsStackSixtyDays(t *testing.T) {
	dsn := os.Getenv("QQ_INVITE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QQ_INVITE_TEST_DATABASE_URL required")
	}
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", parsed.Hostname(), "only dedicated loopback test databases are allowed")
	for _, isolation := range []string{"read committed", "repeatable read"} {
		t.Run(isolation, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			admin, err := sql.Open("postgres", dsn)
			require.NoError(t, err)
			schema := fmt.Sprintf("napcat_redeem_%d", time.Now().UnixNano())
			_, err = admin.ExecContext(ctx, "CREATE SCHEMA "+schema)
			require.NoError(t, err)
			scopedURL := *parsed
			query := scopedURL.Query()
			query.Set("search_path", schema)
			query.Set("default_transaction_isolation", isolation)
			scopedURL.RawQuery = query.Encode()
			db, err := sql.Open("postgres", scopedURL.String())
			require.NoError(t, err)
			db.SetMaxOpenConns(6)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close(); _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE"); _ = admin.Close() })
			require.NoError(t, client.Schema.Create(ctx))
			// Match production's partial unique index, which is intentionally absent
			// from Ent's generic schema. The trigger makes the initial insert overlap.
			_, err = db.ExecContext(ctx, `CREATE UNIQUE INDEX uq_active_user_group ON user_subscriptions(user_id,group_id) WHERE deleted_at IS NULL;
CREATE FUNCTION slow_first_subscription() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(0.2); RETURN NEW; END $$;
CREATE TRIGGER slow_first_subscription BEFORE INSERT ON user_subscriptions FOR EACH ROW EXECUTE FUNCTION slow_first_subscription();`)
			require.NoError(t, err)
			u, err := client.User.Create().SetEmail("redeem@example.test").SetPasswordHash("test-only").Save(ctx)
			require.NoError(t, err)
			g, err := client.Group.Create().SetName("monthly").SetSubscriptionType(service.SubscriptionTypeSubscription).Save(ctx)
			require.NoError(t, err)
			redeems := &redemptionUseBarrierRepo{RedeemCodeRepository: repository.NewRedeemCodeRepository(client), arrived: make(chan struct{}, 2), release: make(chan struct{})}
			for _, code := range []string{"FIRST-MONTH-A", "FIRST-MONTH-B"} {
				require.NoError(t, redeems.Create(ctx, &service.RedeemCode{Code: code, Type: service.RedeemTypeSubscription, Status: service.StatusUnused, GroupID: &g.ID, ValidityDays: 30}))
			}
			subs := service.NewSubscriptionService(repository.NewGroupRepository(client, db), repository.NewUserSubscriptionRepository(client), nil, client, nil)
			defer subs.Stop()
			svc := service.NewRedeemService(redeems, repository.NewUserRepository(client, db), subs, nil, nil, client, nil, nil)
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for _, code := range []string{"FIRST-MONTH-A", "FIRST-MONTH-B"} {
				wg.Go(func() { <-start; _, e := svc.Redeem(ctx, u.ID, code); results <- e })
			}
			close(start)
			// Both redemptions now hold the users FK KEY SHARE from Use. Taking
			// FOR UPDATE afterwards must not create a lock-upgrade deadlock.
			for range 2 {
				select {
				case <-redeems.arrived:
				case <-ctx.Done():
					t.Fatal("redemption Use barrier timed out")
				}
			}
			close(redeems.release)
			wg.Wait()
			close(results)
			for err := range results {
				require.NoError(t, err)
			}
			all, err := client.UserSubscription.Query().Where(usersubscription.UserIDEQ(u.ID), usersubscription.GroupIDEQ(g.ID)).All(ctx)
			require.NoError(t, err)
			require.Len(t, all, 1)
			require.WithinDuration(t, all[0].StartsAt.AddDate(0, 0, 60), all[0].ExpiresAt, time.Second)
			require.NotNil(t, all[0].Notes)
			require.NotContains(t, *all[0].Notes, "FIRST-MONTH-")
			for _, code := range []string{"FIRST-MONTH-A", "FIRST-MONTH-B"} {
				used, e := redeems.GetByCode(ctx, code)
				require.NoError(t, e)
				require.Equal(t, service.StatusUsed, used.Status)
				require.Equal(t, u.ID, *used.UsedBy)
			}
		})
	}
}
