//go:build unit && !integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupRepositoryWelfareGuardPreservesCascadeDependents(t *testing.T) {
	client, db := reductionLockFixtureDatabase(t)
	ctx := context.Background()
	repo := newGroupRepositoryWithSQL(client, db)
	for i, kind := range []string{"campaign", "welfare"} {
		t.Run(kind, func(t *testing.T) {
			limit := 100.0
			g := &service.Group{Name: "guard-" + kind, Platform: service.PlatformAnthropic, RateMultiplier: 1, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeSubscription, MonthlyLimitUSD: &limit}
			require.NoError(t, repo.Create(ctx, g))
			var userID, accountID int64
			require.NoError(t, db.QueryRow(`INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id`, fmt.Sprintf("guard-%d@test.invalid", i)).Scan(&userID))
			require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type) VALUES($1,'anthropic','oauth') RETURNING id`, "guard-"+kind).Scan(&accountID))
			_, err := db.Exec(`INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, accountID, g.ID)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO user_allowed_groups(user_id,group_id) VALUES($1,$2)`, userID, g.ID)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,entitlement_kind,welfare_monthly_limit_usd,welfare_grant_count,monthly_usage_usd) VALUES($1,$2,now(),now()+interval '30 days',$3,100,1,12.5)`, userID, g.ID, map[string]string{"campaign": "standard", "welfare": "welfare"}[kind])
			require.NoError(t, err)
			if kind == "campaign" {
				_, err = db.Exec(`INSERT INTO qq_invite_campaigns(payload) VALUES(jsonb_build_object('subscription_group_id',$1::bigint))`, g.ID)
				require.NoError(t, err)
			}
			before := upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT to_jsonb(s)::text FROM user_subscriptions s WHERE user_id=`+fmt.Sprint(userID))
			_, err = repo.DeleteCascade(ctx, g.ID)
			require.ErrorIs(t, err, service.ErrGroupWelfareReferenced)
			require.Equal(t, fmt.Sprint(g.ID), infraerrors.FromError(err).Metadata["group_id"])
			require.Equal(t, "0", infraerrors.FromError(err).Metadata["unused_supplement_count"])
			if kind == "campaign" {
				require.Equal(t, "1", infraerrors.FromError(err).Metadata["campaign_count"])
			} else {
				require.Equal(t, "1", infraerrors.FromError(err).Metadata["welfare_subscription_count"])
			}
			_, err = repo.DeleteCascadeIfEmpty(ctx, g.ID)
			require.ErrorIs(t, err, service.ErrGroupWelfareReferenced)
			require.ErrorIs(t, repo.Delete(ctx, g.ID), service.ErrGroupWelfareReferenced)
			g.SubscriptionType = service.SubscriptionTypeStandard
			require.ErrorIs(t, repo.Update(ctx, g), service.ErrGroupWelfareReferenced)
			after := upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT to_jsonb(s)::text FROM user_subscriptions s WHERE user_id=`+fmt.Sprint(userID))
			require.Equal(t, before, after)
			var links int
			require.NoError(t, db.QueryRow(`SELECT (SELECT count(*) FROM account_groups WHERE group_id=$1)+(SELECT count(*) FROM user_allowed_groups WHERE group_id=$1)`, g.ID).Scan(&links))
			require.Equal(t, 2, links)
			g.SubscriptionType = service.SubscriptionTypeSubscription
			g.Name = "renamed-" + kind
			newLimit := 300.0
			g.MonthlyLimitUSD = &newLimit
			require.NoError(t, repo.Update(ctx, g))
			require.Equal(t, before, upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT to_jsonb(s)::text FROM user_subscriptions s WHERE user_id=`+fmt.Sprint(userID)))
		})
	}
	t.Run("server_repeatable_read_default", func(t *testing.T) {
		// Keep the setting on this isolated fixture's sole connection. Application
		// mutations must select RC explicitly, regardless of server/session default.
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		_, err := db.Exec(`SET default_transaction_isolation='repeatable read'`)
		require.NoError(t, err)
		g := &service.Group{Name: "rr-unreferenced", Platform: service.PlatformAnthropic, RateMultiplier: 1, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeSubscription}
		require.NoError(t, repo.Create(ctx, g))
		g.SubscriptionType = service.SubscriptionTypeStandard
		require.NoError(t, repo.Update(ctx, g))
		require.NoError(t, repo.Delete(ctx, g.ID))
		g.ID = 0
		g.Name = "rr-cascade"
		require.NoError(t, repo.Create(ctx, g))
		_, err = repo.DeleteCascade(ctx, g.ID)
		require.NoError(t, err)
		g.ID = 0
		g.Name = "caller-owned-rr"
		g.SubscriptionType = service.SubscriptionTypeSubscription
		require.NoError(t, repo.Create(ctx, g))
		tx, err := client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()
		inTx := newGroupRepositoryWithSQL(tx.Client(), tx)
		g.SubscriptionType = service.SubscriptionTypeStandard
		require.ErrorIs(t, inTx.Update(ctx, g), service.ErrGroupWriteRetryRequired)
		require.NoError(t, tx.Rollback())

		// A caller-owned RC transaction must remain usable and uncommitted after
		// the repository update, including Ent's non-sentinel nested-tx error.
		tx, err = client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()
		inTx = newGroupRepositoryWithSQL(tx.Client(), tx)
		require.NoError(t, inTx.Update(ctx, g))
		inside, err := tx.Group.Get(ctx, g.ID)
		require.NoError(t, err)
		require.Equal(t, service.SubscriptionTypeStandard, inside.SubscriptionType)
		require.NoError(t, tx.Rollback())
		outside, err := repo.GetByID(ctx, g.ID)
		require.NoError(t, err)
		require.Equal(t, service.SubscriptionTypeSubscription, outside.SubscriptionType)
	})
}
