//go:build integration

package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestReview3ConcurrentRenewalsAndPaidResetConserveValidity(t *testing.T) {
	resetRepo, original, now := newDailyResetFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewUserSubscriptionRepository(testEntClient(t))
	svc := service.NewSubscriptionService(nil, repo, nil, testEntClient(t), nil)
	t.Cleanup(svc.Stop)

	const renewals = 6
	start := make(chan struct{})
	errs := make(chan error, renewals)
	var wg sync.WaitGroup
	for range renewals {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.ExtendSubscription(ctx, original.ID, 2)
			errs <- err
		}()
	}
	close(start)
	result, resetErr := resetRepo.Apply(ctx, dailyResetCommand(original, now))
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	if resetErr != nil {
		require.ErrorIs(t, resetErr, service.ErrResetStateChanged)
		fresh, err := resetRepo.GetState(ctx, original.UserID, original.ID)
		require.NoError(t, err)
		result, resetErr = resetRepo.Apply(ctx, dailyResetCommand(fresh.Subscription, fresh.ServerTime))
	}
	require.NoError(t, resetErr)
	require.False(t, result.Replayed)
	state, err := resetRepo.GetState(ctx, original.UserID, original.ID)
	require.NoError(t, err)
	require.True(t, state.Subscription.ExpiresAt.Equal(original.ExpiresAt.AddDate(0, 0, renewals*2).Add(-24*time.Hour)),
		"all six extensions and exactly one paid day deduction must survive concurrent writes")
	require.Equal(t, original.DailyResetVersion+renewals+1, state.Subscription.DailyResetVersion)
	require.Equal(t, 1, state.TodayCount)
	require.Zero(t, state.Subscription.DailyUsageUSD)
	require.Equal(t, original.WeeklyUsageUSD, state.Subscription.WeeklyUsageUSD)
	require.Equal(t, original.MonthlyUsageUSD, state.Subscription.MonthlyUsageUSD)
}

func TestReview3RenewalParticipatesInCallerTransactionRollback(t *testing.T) {
	for _, tc := range []struct {
		name    string
		expired bool
		status  string
	}{
		{name: "active", status: service.SubscriptionStatusActive},
		{name: "expired", expired: true, status: service.SubscriptionStatusExpired},
		{name: "suspended_expired", expired: true, status: service.SubscriptionStatusSuspended},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetRepo, sub, now := newDailyResetFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			paid, err := resetRepo.Apply(ctx, dailyResetCommand(sub, now))
			require.NoError(t, err)
			client := testEntClient(t)
			update := client.UserSubscription.UpdateOneID(sub.ID).
				SetStatus(tc.status).SetAutoDailyResetEnabled(true).
				SetPreserveCalendarDailyReset(true).SetDailyUsageUsd(40)
			if tc.expired {
				update.SetExpiresAt(now.Add(-time.Hour))
			}
			_, err = update.Save(ctx)
			require.NoError(t, err)
			repo := NewUserSubscriptionRepository(client)
			original, err := repo.GetByID(ctx, sub.ID)
			require.NoError(t, err)
			svc := service.NewSubscriptionService(nil, repo, nil, client, nil)
			t.Cleanup(svc.Stop)
			tx, err := client.Tx(ctx)
			require.NoError(t, err)
			defer tx.Rollback()
			txCtx := dbent.NewTxContext(ctx, tx)
			renewed, err := svc.ExtendSubscription(txCtx, sub.ID, 3)
			require.NoError(t, err, "renewal must reuse the caller's transaction")
			require.Equal(t, original.DailyResetVersion+1, renewed.DailyResetVersion)
			if tc.expired {
				require.Zero(t, renewed.DailyUsageUSD)
				require.False(t, renewed.AutoDailyResetEnabled)
				require.False(t, renewed.PreserveCalendarDailyReset)
			} else {
				require.True(t, renewed.ExpiresAt.Equal(original.ExpiresAt.AddDate(0, 0, 3)))
			}
			require.NoError(t, tx.Rollback())
			fresh, err := repo.GetByID(ctx, sub.ID)
			require.NoError(t, err)
			require.Equal(t, original, fresh, "rollback must restore expiry, usage, windows, preferences, status, and version")
			event, err := resetRepo.GetOperation(ctx, sub.UserID, sub.ID, paid.Event.OperationID)
			require.NoError(t, err)
			require.Equal(t, paid.Event, event, "rolling back renewal must retain the committed paid reset audit")
		})
	}
}

func TestReview3ExpiredRenewalCannotRestartDailyPaidResetCount(t *testing.T) {
	resetRepo, sub, now := newDailyResetFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO subscription_daily_reset_events
		(subscription_id,user_id,group_id,source,operation_id,request_fingerprint,before_version,after_version,timezone,count_date,day_sequence,before_daily_usage_usd,daily_limit_usd,before_expires_at,after_expires_at,decided_at)
		SELECT $1,$2,$3,'manual','review3-count-'||n,'seed',n*2,n*2+1,'UTC',$4::date,n,100,100,$5::timestamptz,$5::timestamptz-INTERVAL '86400 seconds',$6::timestamptz
		FROM generate_series(1,100) n`, sub.ID, sub.UserID, sub.GroupID, resetDate(now), sub.ExpiresAt, now)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE user_subscriptions SET expires_at = $2,
		status = 'expired', daily_reset_version = 201, auto_daily_reset_enabled = true,
		preserve_calendar_daily_reset = true WHERE id = $1`, sub.ID, now.Add(-time.Hour))
	require.NoError(t, err)
	repo := NewUserSubscriptionRepository(testEntClient(t))
	svc := service.NewSubscriptionService(nil, repo, nil, testEntClient(t), nil)
	t.Cleanup(svc.Stop)
	renewed, err := svc.ExtendSubscription(ctx, sub.ID, 3)
	require.NoError(t, err)
	require.False(t, renewed.AutoDailyResetEnabled)
	require.False(t, renewed.PreserveCalendarDailyReset)
	require.EqualValues(t, 202, renewed.DailyResetVersion)
	require.NoError(t, repo.IncrementUsage(ctx, sub.ID, 60))
	result, err := resetRepo.Apply(ctx, dailyResetCommand(renewed, now))
	require.ErrorIs(t, err, service.ErrResetDailyCountLimit)
	require.Equal(t, 100, result.State.TodayCount)
	require.Equal(t, 60.0, result.State.Subscription.DailyUsageUSD)
	require.True(t, result.State.Subscription.ExpiresAt.Equal(renewed.ExpiresAt))
	require.Equal(t, renewed.DailyResetVersion, result.State.Subscription.DailyResetVersion)
	var events int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT count(*) FROM subscription_daily_reset_events WHERE subscription_id = $1", sub.ID).Scan(&events))
	require.Equal(t, 100, events, "renewing an expired term must not permit a 101st paid reset that calendar day")
}

func TestReview3ContendedResetCancellationReleasesUserAndGroupLocks(t *testing.T) {
	resetRepo, sub, now := newDailyResetFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	blocker, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer blocker.Rollback()
	_, err = blocker.ExecContext(ctx, "SELECT id FROM user_subscriptions WHERE id = $1 FOR UPDATE", sub.ID)
	require.NoError(t, err)
	resetCtx, cancelReset := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelReset()
	_, err = resetRepo.Apply(resetCtx, dailyResetCommand(sub, now))
	require.Error(t, err)
	require.ErrorIs(t, resetCtx.Err(), context.DeadlineExceeded)

	// The subscription remains locked. Updating its user and group must still
	// finish, proving the abandoned reset released earlier shared locks.
	writerCtx, cancelWriter := context.WithTimeout(ctx, 2*time.Second)
	defer cancelWriter()
	_, err = integrationDB.ExecContext(writerCtx, "UPDATE users SET status = 'active' WHERE id = $1", sub.UserID)
	require.NoError(t, err, "cancellation must not leave the user share lock behind")
	_, err = integrationDB.ExecContext(writerCtx, "UPDATE groups SET allow_subscription_day_reset = true WHERE id = $1", sub.GroupID)
	require.NoError(t, err, "cancellation must not leave the group share lock behind")
	require.NoError(t, blocker.Rollback())

	result, err := resetRepo.Apply(ctx, dailyResetCommand(sub, now))
	require.NoError(t, err)
	require.Equal(t, 1, result.State.TodayCount)
	require.Equal(t, sub.ExpiresAt.Add(-24*time.Hour), result.State.Subscription.ExpiresAt)
	require.Equal(t, sub.WeeklyUsageUSD, result.State.Subscription.WeeklyUsageUSD)
	require.Equal(t, sub.MonthlyUsageUSD, result.State.Subscription.MonthlyUsageUSD)
}
