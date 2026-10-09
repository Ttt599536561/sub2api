//go:build integration

package repository

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// The fixture creates accounting facts through the actual mutation service.
// The test clock chooses business dates while PostgreSQL supplies created_at;
// their deliberate difference catches accidental created_at date filtering.
func TestWelfareAdminStatisticsImmutableFacts(t *testing.T) {
	ctx := context.Background()
	prefix := fmt.Sprintf("statistics-%d", time.Now().UnixNano())
	a := mustCreateUser(t, testEntClient(t), &service.User{Email: prefix + "-alice@example.test"})
	b := mustCreateUser(t, testEntClient(t), &service.User{Email: prefix + "-bob@example.test"})
	c := mustCreateUser(t, testEntClient(t), &service.User{Email: prefix + "-percent%literal@example.test"})
	repo := NewWelfareRepository(integrationDB)
	_, err := repo.UpdateSettings(ctx, true)
	require.NoError(t, err)
	now := time.Date(2027, 1, 1, 4, 0, 0, 0, time.UTC)
	s := service.NewWelfareService(repo,
		service.WithWelfareClock(func() time.Time { return now }),
		service.WithWelfareRandom(func(int64) (int64, error) { return 0, nil }))
	for day := 1; day <= 8; day++ {
		now = time.Date(2027, 1, day, 4, 0, 0, 0, time.UTC)
		operation, checkinErr := s.CheckIn(ctx, a.ID)
		require.NoError(t, checkinErr)
		require.Equal(t, "0.10", operation.BaseRewardAmount)
		if day == 7 {
			require.Equal(t, "0.60", operation.StreakRewardAmount)
			replay, replayErr := s.CheckIn(ctx, a.ID)
			require.NoError(t, replayErr)
			require.Equal(t, operation.OperationID, replay.OperationID)
		}
	}
	_, err = integrationDB.Exec(`UPDATE welfare_wallets SET eligible_spend=100 WHERE user_id=$1`, a.ID)
	require.NoError(t, err)
	now = time.Date(2027, 1, 8, 15, 59, 59, 0, time.UTC)
	beforeMidnight, err := s.Draw(ctx, a.ID, "statistics-before-shanghai-midnight")
	require.NoError(t, err)
	require.Equal(t, "2027-01-08", beforeMidnight.Overview.BusinessDate)
	now = time.Date(2027, 1, 8, 16, 0, 0, 0, time.UTC)
	afterMidnight, err := s.Draw(ctx, a.ID, "statistics-after-shanghai-midnight")
	require.NoError(t, err)
	require.Equal(t, "2027-01-09", afterMidnight.Overview.BusinessDate)
	replay, err := s.Draw(ctx, a.ID, "statistics-after-shanghai-midnight")
	require.NoError(t, err)
	require.Equal(t, afterMidnight.OperationID, replay.OperationID)

	high := service.NewWelfareService(repo,
		service.WithWelfareClock(func() time.Time { return now }),
		service.WithWelfareRandom(func(n int64) (int64, error) {
			if n == 11 {
				return 10, nil
			}
			if n == 1000 {
				return 450, nil
			}
			return 0, nil
		}))
	now = time.Date(2027, 1, 7, 4, 0, 0, 0, time.UTC)
	secondCheckin, err := high.CheckIn(ctx, b.ID)
	require.NoError(t, err)
	require.Equal(t, "0.20", secondCheckin.RewardAmount)
	_, err = integrationDB.Exec(`UPDATE welfare_wallets SET eligible_spend=50 WHERE user_id=$1`, b.ID)
	require.NoError(t, err)
	now = time.Date(2027, 1, 9, 4, 0, 0, 0, time.UTC)
	secondDraw, err := high.Draw(ctx, b.ID, "statistics-second-user-draw")
	require.NoError(t, err)
	require.Equal(t, "1.00", secondDraw.RewardAmount)
	_, err = integrationDB.Exec(`INSERT INTO welfare_wallets(user_id,eligible_spend) VALUES($1,50)`, c.ID)
	require.NoError(t, err)
	_, err = s.Draw(ctx, c.ID, "statistics-only-draw-user")
	require.NoError(t, err)
	quote, err := s.Quote(ctx, a.ID, "all", "")
	require.NoError(t, err)
	require.Equal(t, "2.40", quote.Amount)
	_, err = s.Redeem(ctx, a.ID, "statistics-redeem-does-not-change-issued-total", quote.Amount, quote.WelfareBalanceVersion)
	require.NoError(t, err)
	var redeemed int64
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM welfare_ledger WHERE user_id=$1 AND type='redeem'`, a.ID).Scan(&redeemed))
	require.EqualValues(t, 1, redeemed)

	filter := service.WelfareStatisticsFilter{DateFrom: "2027-01-07", DateTo: "2027-01-11", Search: prefix}
	expected := service.WelfareRewardTotals{
		DailyAmount: "0.40", StreakAmount: "0.60", CheckinAmount: "1.00", DrawAmount: "2.50", TotalAmount: "3.50",
		CheckinUsers: 2, CheckinCount: 3, StreakUsers: 1, DrawUsers: 3, DrawCount: 4, ParticipatingUsers: 3,
	}
	zero := service.WelfareRewardTotals{DailyAmount: "0.00", StreakAmount: "0.00", CheckinAmount: "0.00", DrawAmount: "0.00", TotalAmount: "0.00"}
	statistics, err := s.AdminStatistics(ctx, filter)
	require.NoError(t, err)
	require.Equal(t, "Asia/Shanghai", statistics.Timezone)
	require.Equal(t, expected, statistics.Summary)
	require.Len(t, statistics.Daily, 5)
	require.Equal(t, "2027-01-07", statistics.Daily[0].Date)
	require.Equal(t, "0.90", statistics.Daily[0].TotalAmount)
	require.Equal(t, "0.60", statistics.Daily[1].TotalAmount)
	require.Equal(t, "2.00", statistics.Daily[2].TotalAmount)
	require.Equal(t, zero, statistics.Daily[3].WelfareRewardTotals)
	require.Equal(t, zero, statistics.Daily[4].WelfareRewardTotals)
	for _, amount := range []struct {
		name    string
		summary string
		daily   func(service.WelfareStatisticsDay) string
	}{
		{"daily", expected.DailyAmount, func(day service.WelfareStatisticsDay) string { return day.DailyAmount }},
		{"streak", expected.StreakAmount, func(day service.WelfareStatisticsDay) string { return day.StreakAmount }},
		{"checkin", expected.CheckinAmount, func(day service.WelfareStatisticsDay) string { return day.CheckinAmount }},
		{"draw", expected.DrawAmount, func(day service.WelfareStatisticsDay) string { return day.DrawAmount }},
		{"total", expected.TotalAmount, func(day service.WelfareStatisticsDay) string { return day.TotalAmount }},
	} {
		sum := decimal.Zero
		for _, day := range statistics.Daily {
			sum = sum.Add(decimal.RequireFromString(amount.daily(day)))
		}
		require.Equal(t, amount.summary, sum.StringFixed(2), amount.name)
	}

	t.Run("period_and_historical_totals", func(t *testing.T) {
		result, err := s.AdminStatisticsUsers(ctx, service.WelfareStatisticsUsersFilter{WelfareStatisticsFilter: filter, PageSize: 20})
		require.NoError(t, err)
		require.EqualValues(t, 3, result.Total)
		require.Len(t, result.Items, 3)
		require.Equal(t, a.ID, result.Items[0].UserID)
		require.Equal(t, "1.80", result.Items[0].Period.TotalAmount)
		require.EqualValues(t, 2, result.Items[0].Period.CheckinCount)
		require.Equal(t, "2.40", result.Items[0].Lifetime.TotalAmount)
		require.Equal(t, "0.80", result.Items[0].Lifetime.DailyAmount)
		require.EqualValues(t, 8, result.Items[0].Lifetime.CheckinCount)
	})
	t.Run("zero_period_preserves_history_and_historical_checkin_sort", func(t *testing.T) {
		noActivity := filter
		noActivity.DateFrom, noActivity.DateTo = "2027-01-10", "2027-01-11"
		result, err := s.AdminStatisticsUsers(ctx, service.WelfareStatisticsUsersFilter{WelfareStatisticsFilter: noActivity, PageSize: 20, SortBy: "checkin_count", SortOrder: "desc"})
		require.NoError(t, err)
		require.EqualValues(t, 3, result.Total)
		require.Len(t, result.Items, 3)
		require.Equal(t, []int64{a.ID, b.ID, c.ID}, []int64{result.Items[0].UserID, result.Items[1].UserID, result.Items[2].UserID})
		for _, item := range result.Items {
			require.Equal(t, zero, item.Period)
		}
		require.Equal(t, "2.40", result.Items[0].Lifetime.TotalAmount)
		require.EqualValues(t, 8, result.Items[0].Lifetime.CheckinCount)
		noActivity.Search = prefix + "-alice"
		onlyAlice, err := s.AdminStatisticsUsers(ctx, service.WelfareStatisticsUsersFilter{WelfareStatisticsFilter: noActivity})
		require.NoError(t, err)
		require.EqualValues(t, 1, onlyAlice.Total)
		require.Equal(t, "2.40", onlyAlice.Items[0].Lifetime.TotalAmount)
	})
	t.Run("literal_search_and_user_identity", func(t *testing.T) {
		literal := filter
		literal.Search = "%"
		result, err := s.AdminStatisticsUsers(ctx, service.WelfareStatisticsUsersFilter{WelfareStatisticsFilter: literal})
		require.NoError(t, err)
		require.EqualValues(t, 1, result.Total, "percent is an email character, not a wildcard")
		require.Equal(t, c.ID, result.Items[0].UserID)
		numeric := filter
		numeric.Search = strconv.FormatInt(b.ID, 10)
		numeric.UserID = b.ID
		summary, err := s.AdminStatistics(ctx, numeric)
		require.NoError(t, err)
		require.Equal(t, "1.20", summary.Summary.TotalAmount)
		require.EqualValues(t, 1, summary.Summary.ParticipatingUsers)
	})
	t.Run("stable_server_pagination", func(t *testing.T) {
		seen := map[int64]bool{}
		for page := 1; page <= 3; page++ {
			query := service.WelfareStatisticsUsersFilter{WelfareStatisticsFilter: filter, Page: page, PageSize: 1, SortBy: "period_total_amount", SortOrder: "desc"}
			result, err := s.AdminStatisticsUsers(ctx, query)
			require.NoError(t, err)
			require.EqualValues(t, 3, result.Total)
			require.Len(t, result.Items, 1)
			require.False(t, seen[result.Items[0].UserID])
			seen[result.Items[0].UserID] = true
			repeated, err := s.AdminStatisticsUsers(ctx, query)
			require.NoError(t, err)
			require.Equal(t, result.Items, repeated.Items)
		}
		ties := filter
		ties.DateFrom, ties.DateTo = "2027-01-10", "2027-01-11"
		ascending, err := s.AdminStatisticsUsers(ctx, service.WelfareStatisticsUsersFilter{WelfareStatisticsFilter: ties, SortBy: "period_total_amount", SortOrder: "asc"})
		require.NoError(t, err)
		require.Equal(t, []int64{a.ID, b.ID, c.ID}, []int64{ascending.Items[0].UserID, ascending.Items[1].UserID, ascending.Items[2].UserID})
		records, err := s.AdminStatisticsRecords(ctx, service.WelfareStatisticsRecordsFilter{WelfareStatisticsFilter: filter, PageSize: 100})
		require.NoError(t, err)
		require.EqualValues(t, 8, records.Total)
		require.Len(t, records.Items, 8, "redeem must not enter reward records")
		seenRecords := map[string]bool{}
		var collected []service.WelfareStatisticsRecord
		for page := 1; page <= 3; page++ {
			query := service.WelfareStatisticsRecordsFilter{WelfareStatisticsFilter: filter, Page: page, PageSize: 3}
			result, err := s.AdminStatisticsRecords(ctx, query)
			require.NoError(t, err)
			require.EqualValues(t, 8, result.Total)
			for _, record := range result.Items {
				require.NotEqual(t, "redeem", record.Type)
				require.NotEmpty(t, record.ID)
				require.Contains(t, []int64{a.ID, b.ID, c.ID}, record.UserID)
				require.GreaterOrEqual(t, record.BusinessDate, filter.DateFrom)
				require.LessOrEqual(t, record.BusinessDate, filter.DateTo)
				require.False(t, seenRecords[record.ID])
				seenRecords[record.ID] = true
				collected = append(collected, record)
			}
			repeated, err := s.AdminStatisticsRecords(ctx, query)
			require.NoError(t, err)
			require.Equal(t, result.Items, repeated.Items)
		}
		require.Len(t, seenRecords, 8)
		require.Equal(t, records.Items, collected, "paged rows retain the complete query order")
		for i := 1; i < len(collected); i++ {
			require.False(t, collected[i].CreatedAt.After(collected[i-1].CreatedAt))
		}
	})
	t.Run("business_day_boundary_type_and_milestone", func(t *testing.T) {
		for _, date := range []string{"2027-01-08", "2027-01-09"} {
			scoped := service.WelfareStatisticsFilter{DateFrom: date, DateTo: date, UserID: a.ID}
			result, err := s.AdminStatisticsRecords(ctx, service.WelfareStatisticsRecordsFilter{WelfareStatisticsFilter: scoped, Type: "draw"})
			require.NoError(t, err)
			require.EqualValues(t, 1, result.Total)
			require.Equal(t, date, result.Items[0].BusinessDate)
			require.Equal(t, "0.50", result.Items[0].Amount)
		}
		result, err := s.AdminStatisticsRecords(ctx, service.WelfareStatisticsRecordsFilter{WelfareStatisticsFilter: filter, Type: "streak"})
		require.NoError(t, err)
		require.EqualValues(t, 1, result.Total)
		require.Equal(t, "0.60", result.Items[0].Amount)
		require.NotNil(t, result.Items[0].CycleDay)
		require.EqualValues(t, 7, *result.Items[0].CycleDay)
	})
	t.Run("soft_deleted_user_keeps_historical_facts", func(t *testing.T) {
		_, err := integrationDB.Exec(`UPDATE users SET deleted_at=NOW() WHERE id=$1`, c.ID)
		require.NoError(t, err)
		result, err := s.AdminStatistics(ctx, filter)
		require.NoError(t, err)
		require.Equal(t, expected, result.Summary)
		deleted := filter
		deleted.UserID = c.ID
		userResult, err := s.AdminStatisticsUsers(ctx, service.WelfareStatisticsUsersFilter{WelfareStatisticsFilter: deleted})
		require.NoError(t, err)
		require.EqualValues(t, 1, userResult.Total)
		require.Equal(t, c.Email, userResult.Items[0].Email)
		require.Equal(t, "0.50", userResult.Items[0].Lifetime.TotalAmount)
		recordResult, err := s.AdminStatisticsRecords(ctx, service.WelfareStatisticsRecordsFilter{WelfareStatisticsFilter: deleted})
		require.NoError(t, err)
		require.EqualValues(t, 1, recordResult.Total)
	})
	t.Run("empty_search_has_exact_zeros", func(t *testing.T) {
		empty := filter
		empty.Search = prefix + "-no-such-user"
		result, err := s.AdminStatistics(ctx, empty)
		require.NoError(t, err)
		require.Equal(t, zero, result.Summary)
		require.Len(t, result.Daily, 5)
		userResult, err := s.AdminStatisticsUsers(ctx, service.WelfareStatisticsUsersFilter{WelfareStatisticsFilter: empty})
		require.NoError(t, err)
		require.Empty(t, userResult.Items)
		require.Zero(t, userResult.Total)
		recordResult, err := s.AdminStatisticsRecords(ctx, service.WelfareStatisticsRecordsFilter{WelfareStatisticsFilter: empty})
		require.NoError(t, err)
		require.Empty(t, recordResult.Items)
		require.Zero(t, recordResult.Total)
	})
	t.Run("real_readonly_repeatable_read_snapshot", func(t *testing.T) {
		snapshot, err := repo.(*welfareRepository).statisticsSnapshot(ctx)
		require.NoError(t, err)
		defer func() { _ = snapshot.Rollback() }()
		var isolation, readOnly string
		require.NoError(t, snapshot.QueryRowContext(ctx, `SHOW transaction_isolation`).Scan(&isolation))
		require.NoError(t, snapshot.QueryRowContext(ctx, `SHOW transaction_read_only`).Scan(&readOnly))
		require.Equal(t, "repeatable read", isolation)
		require.Equal(t, "on", readOnly)
		where, args := welfareStatisticsWhere(filter, true, nil)
		plan, err := snapshot.QueryContext(ctx, `EXPLAIN SELECT l.id FROM welfare_ledger l JOIN users u ON u.id=l.user_id WHERE `+where, args...)
		require.NoError(t, err)
		defer plan.Close()
		for plan.Next() {
			var line string
			require.NoError(t, plan.Scan(&line))
			t.Log(line)
		}
		require.NoError(t, plan.Err())
	})
}
