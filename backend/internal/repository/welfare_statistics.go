package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

var _ service.WelfareStatisticsRepository = (*welfareRepository)(nil)

// Aggregating numeric cents avoids both floating point rounding and bigint SUM overflow.
func welfareStatisticsAggregates(condition string) string {
	amount := func(kind string) string {
		return `COALESCE(SUM(l.amount_cents::numeric) FILTER (WHERE l.type='` + kind + `'` + condition + `),0)::text`
	}
	count := func(expression, predicate string) string {
		return `COUNT(` + expression + `) FILTER (WHERE ` + predicate + condition + `)`
	}
	return strings.Join([]string{
		amount("daily"), amount("streak"), amount("draw"),
		count("DISTINCT l.user_id", "l.type IN ('daily','streak')"), count("*", "l.type='daily'"),
		count("DISTINCT l.user_id", "l.type='streak'"), count("DISTINCT l.user_id", "l.type='draw'"), count("*", "l.type='draw'"),
		count("DISTINCT l.user_id", "l.type IN ('daily','streak','draw')"),
	}, ",")
}

type welfareStatisticsTotalsScan struct {
	daily, streak, draw string
	totals              service.WelfareRewardTotals
}

func (s *welfareStatisticsTotalsScan) targets() []any {
	return []any{&s.daily, &s.streak, &s.draw, &s.totals.CheckinUsers, &s.totals.CheckinCount, &s.totals.StreakUsers, &s.totals.DrawUsers, &s.totals.DrawCount, &s.totals.ParticipatingUsers}
}
func (s *welfareStatisticsTotalsScan) result() (service.WelfareRewardTotals, error) {
	daily, err := decimal.NewFromString(s.daily)
	if err != nil {
		return s.totals, err
	}
	streak, err := decimal.NewFromString(s.streak)
	if err != nil {
		return s.totals, err
	}
	draw, err := decimal.NewFromString(s.draw)
	if err != nil {
		return s.totals, err
	}
	s.totals.DailyAmount = daily.Shift(-2).StringFixed(2)
	s.totals.StreakAmount = streak.Shift(-2).StringFixed(2)
	s.totals.DrawAmount = draw.Shift(-2).StringFixed(2)
	s.totals.CheckinAmount = daily.Add(streak).Shift(-2).StringFixed(2)
	s.totals.TotalAmount = daily.Add(streak).Add(draw).Shift(-2).StringFixed(2)
	return s.totals, nil
}
func welfareStatisticsWhere(filter service.WelfareStatisticsFilter, dates bool, args []any) (string, []any) {
	where := `l.type IN ('daily','streak','draw')`
	bind := func(value any) string { args = append(args, value); return fmt.Sprintf("$%d", len(args)) }
	if dates {
		where += ` AND l.business_date >= ` + bind(filter.DateFrom) + `::date AND l.business_date <= ` + bind(filter.DateTo) + `::date`
	}
	if filter.UserID > 0 {
		where += ` AND l.user_id = ` + bind(filter.UserID)
	}
	if filter.Search != "" {
		// LIKE metacharacters must match literal email characters supplied by the administrator.
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(filter.Search)
		email := `u.email ILIKE ` + bind("%"+escaped+"%") + ` ESCAPE '\'`
		digits := true
		for _, c := range filter.Search {
			if c < '0' || c > '9' {
				digits = false
				break
			}
		}
		if id, err := strconv.ParseInt(filter.Search, 10, 64); digits && err == nil && id > 0 {
			where += ` AND (` + email + ` OR l.user_id = ` + bind(id) + `)`
		} else {
			where += ` AND ` + email
		}
	}
	return where, args
}
func (r *welfareRepository) statisticsSnapshot(ctx context.Context) (*sql.Tx, error) {
	return r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
}
func (r *welfareRepository) AdminStatistics(ctx context.Context, filter service.WelfareStatisticsFilter) (*service.WelfareStatistics, error) {
	tx, err := r.statisticsSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	where, args := welfareStatisticsWhere(filter, true, nil)
	query := `SELECT COALESCE(l.business_date::text,''),` + welfareStatisticsAggregates("") + ` FROM welfare_ledger l JOIN users u ON u.id=l.user_id WHERE ` + where + ` GROUP BY GROUPING SETS ((),(l.business_date)) ORDER BY l.business_date NULLS FIRST`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := &service.WelfareStatistics{DateFrom: filter.DateFrom, DateTo: filter.DateTo, Timezone: "Asia/Shanghai", Daily: []service.WelfareStatisticsDay{}}
	for rows.Next() {
		var date string
		var scan welfareStatisticsTotalsScan
		targets := append([]any{&date}, scan.targets()...)
		if err = rows.Scan(targets...); err != nil {
			return nil, err
		}
		totals, err := scan.result()
		if err != nil {
			return nil, err
		}
		if date == "" {
			result.Summary = totals
		} else {
			result.Daily = append(result.Daily, service.WelfareStatisticsDay{Date: date, WelfareRewardTotals: totals})
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
func (r *welfareRepository) AdminStatisticsUsers(ctx context.Context, filter service.WelfareStatisticsUsersFilter) (*service.WelfareStatisticsUsers, error) {
	tx, err := r.statisticsSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	result := &service.WelfareStatisticsUsers{Items: []service.WelfareStatisticsUser{}, Page: filter.Page, PageSize: filter.PageSize}
	where, args := welfareStatisticsWhere(filter.WelfareStatisticsFilter, false, nil)
	base := ` FROM welfare_ledger l JOIN users u ON u.id=l.user_id WHERE `
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT l.user_id)`+base+where, args...).Scan(&result.Total); err != nil {
		return nil, err
	}
	where, args = welfareStatisticsWhere(filter.WelfareStatisticsFilter, false, []any{filter.DateFrom, filter.DateTo})
	period := ` AND l.business_date >= $1::date AND l.business_date <= $2::date`
	order := `SUM(l.amount_cents::numeric)`
	switch filter.SortBy {
	case "period_total_amount":
		order = `COALESCE(SUM(l.amount_cents::numeric) FILTER (WHERE l.business_date >= $1::date AND l.business_date <= $2::date),0)`
	case "checkin_count":
		order = `COUNT(*) FILTER (WHERE l.type='daily')`
	}
	direction := "DESC"
	if filter.SortOrder == "asc" {
		direction = "ASC"
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	query := `SELECT l.user_id,u.email,` + welfareStatisticsAggregates(period) + `,` + welfareStatisticsAggregates("") + base + where + ` GROUP BY l.user_id,u.email ORDER BY ` + order + ` ` + direction + `,l.user_id ASC LIMIT $` + strconv.Itoa(len(args)-1) + ` OFFSET $` + strconv.Itoa(len(args))
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item service.WelfareStatisticsUser
		var p, l welfareStatisticsTotalsScan
		targets := append([]any{&item.UserID, &item.Email}, p.targets()...)
		targets = append(targets, l.targets()...)
		if err = rows.Scan(targets...); err != nil {
			return nil, err
		}
		if item.Period, err = p.result(); err != nil {
			return nil, err
		}
		if item.Lifetime, err = l.result(); err != nil {
			return nil, err
		}
		result.Items = append(result.Items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
func (r *welfareRepository) AdminStatisticsRecords(ctx context.Context, filter service.WelfareStatisticsRecordsFilter) (*service.WelfareStatisticsRecords, error) {
	tx, err := r.statisticsSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	result := &service.WelfareStatisticsRecords{Items: []service.WelfareStatisticsRecord{}, Page: filter.Page, PageSize: filter.PageSize}
	where, args := welfareStatisticsWhere(filter.WelfareStatisticsFilter, true, nil)
	if filter.Type != "all" && filter.Type != "" {
		args = append(args, filter.Type)
		where += ` AND l.type=$` + strconv.Itoa(len(args))
	}
	base := ` FROM welfare_ledger l JOIN users u ON u.id=l.user_id`
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*)`+base+` WHERE `+where, args...).Scan(&result.Total); err != nil {
		return nil, err
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	query := `SELECT l.id::text,l.user_id,u.email,l.type,l.created_at,l.business_date::text,l.amount_cents::text,c.cycle_day` + base + ` LEFT JOIN welfare_checkins c ON c.operation_id=l.operation_id AND l.type IN ('daily','streak') WHERE ` + where + ` ORDER BY l.created_at DESC,l.id DESC LIMIT $` + strconv.Itoa(len(args)-1) + ` OFFSET $` + strconv.Itoa(len(args))
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item service.WelfareStatisticsRecord
		var amount string
		var cycle sql.NullInt64
		if err = rows.Scan(&item.ID, &item.UserID, &item.Email, &item.Type, &item.CreatedAt, &item.BusinessDate, &amount, &cycle); err != nil {
			return nil, err
		}
		cents, err := decimal.NewFromString(amount)
		if err != nil {
			return nil, err
		}
		item.Amount = cents.Shift(-2).StringFixed(2)
		if cycle.Valid {
			item.CycleDay = &cycle.Int64
		}
		result.Items = append(result.Items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
