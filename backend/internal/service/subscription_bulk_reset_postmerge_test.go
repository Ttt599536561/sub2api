//go:build unit

package service_test

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
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func postmergeResetEntFixture(t *testing.T) (*dbent.Client, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	_, err = db.Exec(`CREATE TABLE scheduler_outbox (
		id INTEGER PRIMARY KEY, event_type TEXT NOT NULL, account_id BIGINT,
		group_id BIGINT, payload BLOB, dedup_key TEXT);
		CREATE UNIQUE INDEX scheduler_outbox_dedup ON scheduler_outbox(dedup_key) WHERE dedup_key IS NOT NULL`)
	require.NoError(t, err)
	return client, db
}

func postmergeResetSubscription(t *testing.T, client *dbent.Client, suffix, status string, expires time.Time) *dbent.UserSubscription {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	user, err := client.User.Create().SetEmail(suffix + "@postmerge.test").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	group, err := client.Group.Create().SetName(suffix).SetSubscriptionType(service.SubscriptionTypeSubscription).
		SetAllowSubscriptionDayReset(true).SetDailyLimitUsd(100).Save(ctx)
	require.NoError(t, err)
	window := now.Add(-2 * time.Hour)
	sub, err := client.UserSubscription.Create().SetUserID(user.ID).SetGroupID(group.ID).
		SetStatus(status).SetStartsAt(now.Add(-10 * 24 * time.Hour)).SetExpiresAt(expires).
		SetAutoDailyResetEnabled(true).SetPreserveCalendarDailyReset(true).SetDailyResetVersion(7).
		SetDailyWindowStart(window).SetWeeklyWindowStart(window).SetMonthlyWindowStart(window).
		SetDailyUsageUsd(40).SetWeeklyUsageUsd(70).SetMonthlyUsageUsd(90).Save(ctx)
	require.NoError(t, err)
	_, err = client.SubscriptionDailyResetEvent.Create().SetSubscriptionID(sub.ID).SetUserID(user.ID).SetGroupID(group.ID).
		SetSource("manual").SetOperationID("previous-" + suffix).SetRequestFingerprint("fingerprint").
		SetBeforeVersion(6).SetAfterVersion(7).SetTimezone("UTC").SetCountDate(now).SetDaySequence(100).
		SetBeforeDailyUsageUsd(100).SetDailyLimitUsd(100).
		SetBeforeExpiresAt(expires.Add(24 * time.Hour)).SetAfterExpiresAt(expires).SetDecidedAt(now.Add(-2 * time.Hour)).Save(ctx)
	require.NoError(t, err)
	return sub
}

func TestPostmergeBulkAdminResetPreservesPaidResetPreferencesAndAudit(t *testing.T) {
	client, _ := postmergeResetEntFixture(t)
	ctx := context.Background()
	sub := postmergeResetSubscription(t, client, "reset-quota", service.SubscriptionStatusActive, time.Now().UTC().Add(30*24*time.Hour).Truncate(time.Microsecond))
	repo := repository.NewUserSubscriptionRepository(client)
	svc := service.NewSubscriptionService(nil, repo, nil, client, nil)
	t.Cleanup(svc.Stop)
	result, err := svc.BulkSubscriptionAction(ctx, &service.BulkSubscriptionActionInput{
		SubscriptionIDs: []int64{sub.ID, sub.ID}, Action: "reset_quota", Daily: true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.SuccessCount)
	require.Zero(t, result.FailedCount)
	fresh, err := repo.GetByID(ctx, sub.ID)
	require.NoError(t, err)
	require.EqualValues(t, 8, fresh.DailyResetVersion)
	require.Zero(t, fresh.DailyUsageUSD)
	require.Equal(t, 70.0, fresh.WeeklyUsageUSD)
	require.Equal(t, 90.0, fresh.MonthlyUsageUSD)
	require.True(t, fresh.ExpiresAt.Equal(sub.ExpiresAt), "admin quota reset does not deduct a day")
	require.True(t, fresh.AutoDailyResetEnabled)
	require.True(t, fresh.PreserveCalendarDailyReset)
	require.True(t, fresh.WeeklyWindowStart.Equal(*sub.WeeklyWindowStart))
	require.True(t, fresh.MonthlyWindowStart.Equal(*sub.MonthlyWindowStart))
	event, err := client.SubscriptionDailyResetEvent.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, 100, event.DaySequence, "admin reset must not erase or restart the paid reset daily count")
	require.EqualValues(t, 7, event.AfterVersion)
}

func TestPostmergeGroupResetPermissionPersistsThroughEntRepositoryAndDTO(t *testing.T) {
	client, db := postmergeResetEntFixture(t)
	ctx := context.Background()
	limit := 100.0
	group := &service.Group{Name: "monthly-roundtrip", Platform: service.PlatformOpenAI, Status: service.StatusActive,
		RateMultiplier: 1, ImageRateMultiplier: 1, VideoRateMultiplier: 1, PeakRateMultiplier: 1,
		SubscriptionType: service.SubscriptionTypeSubscription, AllowSubscriptionDayReset: true, DailyLimitUSD: &limit}
	repo := repository.NewGroupRepository(client, db)
	require.NoError(t, repo.Create(ctx, group))
	fresh, err := repo.GetByID(ctx, group.ID)
	require.NoError(t, err)
	require.True(t, fresh.AllowSubscriptionDayReset)
	require.True(t, dto.GroupFromServiceAdmin(fresh).AllowSubscriptionDayReset)
	require.True(t, dto.GroupFromServiceShallow(fresh).AllowSubscriptionDayReset)
	fresh.AllowSubscriptionDayReset = false
	require.NoError(t, repo.Update(ctx, fresh))
	fresh, err = repo.GetByID(ctx, group.ID)
	require.NoError(t, err)
	require.False(t, fresh.AllowSubscriptionDayReset)
	require.False(t, dto.GroupFromServiceAdmin(fresh).AllowSubscriptionDayReset)
	stored, err := client.Group.Get(ctx, group.ID)
	require.NoError(t, err)
	require.False(t, stored.AllowSubscriptionDayReset)
}
