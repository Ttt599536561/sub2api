//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func resetLockRows(now time.Time, version int64) *sqlmock.Rows {
	data, _ := json.Marshal(map[string]any{"id": 3, "user_id": 1, "group_id": 2,
		"starts_at": now.Add(-24 * time.Hour), "expires_at": now.Add(10 * 24 * time.Hour), "status": "active",
		"daily_window_start": now.Truncate(24 * time.Hour), "weekly_window_start": now, "monthly_window_start": now,
		"daily_usage_usd": 60, "weekly_usage_usd": 300, "monthly_usage_usd": 800,
		"auto_daily_reset_enabled": true, "daily_reset_version": version, "preserve_calendar_daily_reset": false})
	return sqlmock.NewRows([]string{"subscription"}).AddRow(data)
}

func expectResetLocks(mock sqlmock.Sqlmock, now time.Time, version int64) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT status, deleted_at FROM users.*FOR SHARE").WithArgs(int64(1)).WillReturnRows(
		sqlmock.NewRows([]string{"status", "deleted_at"}).AddRow("active", nil))
	mock.ExpectQuery("(?s)SELECT.*FROM groups.*FOR SHARE").WithArgs(int64(3), int64(1)).WillReturnRows(
		sqlmock.NewRows([]string{"group"}).AddRow(`{"id":2,"name":"Monthly card","status":"active","subscription_type":"subscription","allow_subscription_day_reset":true,"daily_limit_usd":100}`))
	mock.ExpectQuery("(?s)SELECT.*FROM user_subscriptions.*FOR UPDATE").WithArgs(int64(3), int64(1), int64(2)).WillReturnRows(resetLockRows(now, version))
}

func expectResetClockAndUser(mock sqlmock.Sqlmock, now time.Time) {
	mock.ExpectQuery("SELECT clock_timestamp").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now))
	mock.ExpectQuery("SELECT COUNT").WithArgs(int64(3), now.Format("2006-01-02")).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
}

func TestSubscriptionDailyResetRepository_StaleRoundDoesNotMutate(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	expectResetLocks(mock, now, 11)
	mock.ExpectQuery("(?s)SELECT.*FROM subscription_daily_reset_events").WillReturnError(sql.ErrNoRows)
	expectResetClockAndUser(mock, now)
	mock.ExpectCommit()
	result, err := NewSubscriptionDailyResetRepository(nil, db).Apply(context.Background(), &service.SubscriptionDailyResetCommand{
		UserID: 1, SubscriptionID: 3, ExpectedVersion: 10, OperationID: "stale", ObservedDate: "2026-09-06", Source: "manual", RequestFingerprint: "fingerprint",
	})
	require.ErrorIs(t, err, service.ErrResetStateChanged)
	require.NotNil(t, result.State)
	require.EqualValues(t, 11, result.State.Subscription.DailyResetVersion)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionDailyResetRepository_EventFailureRollsBackDeduction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	expectResetLocks(mock, now, 10)
	mock.ExpectQuery("(?s)SELECT.*FROM subscription_daily_reset_events").WillReturnError(sql.ErrNoRows)
	expectResetClockAndUser(mock, now)
	mock.ExpectExec("(?s)UPDATE user_subscriptions.*daily_usage_usd").WillReturnResult(sqlmock.NewResult(0, 1))
	injected := errors.New("event storage unavailable")
	mock.ExpectQuery("(?s)INSERT INTO subscription_daily_reset_events").WillReturnError(injected)
	mock.ExpectRollback()
	_, err = NewSubscriptionDailyResetRepository(nil, db).Apply(context.Background(), &service.SubscriptionDailyResetCommand{
		UserID: 1, SubscriptionID: 3, ExpectedVersion: 10, OperationID: "rollback", ObservedDate: "2026-09-06", Source: "manual", RequestFingerprint: "fingerprint",
	})
	require.ErrorIs(t, err, injected)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionDailyResetRepository_RetriesContendedLocksInNewTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT status, deleted_at FROM users.*FOR SHARE").WithArgs(int64(1)).WillReturnRows(
		sqlmock.NewRows([]string{"status", "deleted_at"}).AddRow("active", nil))
	mock.ExpectQuery("(?s)SELECT.*FROM groups.*FOR SHARE").WithArgs(int64(3), int64(1)).
		WillReturnError(&pq.Error{Code: "55P03"})
	mock.ExpectRollback()
	expectResetLocks(mock, now, 10)
	expectResetClockAndUser(mock, now)
	mock.ExpectCommit()
	state, err := NewSubscriptionDailyResetRepository(nil, db).GetState(context.Background(), 1, 3)
	require.NoError(t, err)
	require.EqualValues(t, 10, state.Subscription.DailyResetVersion)
	require.NoError(t, mock.ExpectationsWereMet())
}
