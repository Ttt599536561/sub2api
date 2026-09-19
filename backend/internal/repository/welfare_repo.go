package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type welfareRepository struct{ db *sql.DB }

func NewWelfareRepository(db *sql.DB) service.WelfareRepository { return &welfareRepository{db: db} }

type welfareRowScanner interface{ Scan(...any) error }

const welfareWalletColumns = `user_id,balance_cents,total_earned_cents,total_redeemed_cents,total_checkin_days,cycle_id,cycle_day,last_checkin_date,daily_low_count,eligible_spend::text,draws_used,wallet_version,welfare_balance_version`

func scanWelfareWallet(row welfareRowScanner) (service.WelfareWallet, error) {
	var w service.WelfareWallet
	var last sql.NullTime
	err := row.Scan(&w.UserID, &w.BalanceCents, &w.TotalEarnedCents, &w.TotalRedeemedCents, &w.TotalCheckinDays, &w.CycleID, &w.CycleDay, &last, &w.DailyLowCount, &w.EligibleSpend, &w.DrawsUsed, &w.WalletVersion, &w.WelfareBalanceVersion)
	if last.Valid {
		w.LastCheckinDate = last.Time.Format("2006-01-02")
	}
	return w, err
}

func scanWelfareSettings(row welfareRowScanner) (*service.WelfareSettings, error) {
	p := &service.WelfareSettings{RulesVersion: 2}
	var launch sql.NullTime
	if err := row.Scan(&p.Enabled, &launch); err != nil {
		return nil, err
	}
	if launch.Valid {
		p.LaunchAt = &launch.Time
	}
	return p, nil
}

func (r *welfareRepository) GetSettings(ctx context.Context) (*service.WelfareSettings, error) {
	return scanWelfareSettings(r.db.QueryRowContext(ctx, `SELECT enabled,launch_at FROM welfare_programs WHERE id=1`))
}

func (r *welfareRepository) UpdateSettings(ctx context.Context, enabled bool) (*service.WelfareSettings, error) {
	return scanWelfareSettings(r.db.QueryRowContext(ctx, `UPDATE welfare_programs SET enabled=$1,launch_at=CASE WHEN $1 THEN COALESCE(launch_at,clock_timestamp()) ELSE launch_at END,updated_at=NOW() WHERE id=1 RETURNING enabled,launch_at`, enabled))
}

// The user row is always first: gateway billing and redemption update that row
// before the welfare wallet, avoiding the inverse-order deadlock.
func welfareLockedState(ctx context.Context, tx *sql.Tx, userID int64) (*service.WelfareState, error) {
	state := &service.WelfareState{}
	err := tx.QueryRowContext(ctx, `SELECT balance::text FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, userID).Scan(&state.AccountBalance)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO welfare_wallets(user_id) VALUES($1) ON CONFLICT DO NOTHING`, userID); err != nil {
		return nil, err
	}
	state.Wallet, err = scanWelfareWallet(tx.QueryRowContext(ctx, `SELECT `+welfareWalletColumns+` FROM welfare_wallets WHERE user_id=$1 FOR UPDATE`, userID))
	if err != nil {
		return nil, err
	}
	p, err := scanWelfareSettings(tx.QueryRowContext(ctx, `SELECT enabled,launch_at FROM welfare_programs WHERE id=1 FOR SHARE`))
	if err != nil {
		return nil, err
	}
	state.Program = *p
	return state, nil
}

func (r *welfareRepository) State(ctx context.Context, userID int64) (*service.WelfareState, error) {
	// A single statement is a consistent snapshot of both balances and the
	// program, including an implicit zero wallet for existing users. Polling the
	// overview never writes and never waits for a billing row lock.
	state := &service.WelfareState{}
	w := &state.Wallet
	var last, launch sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT u.id,u.balance::text,
		COALESCE(w.balance_cents,0),COALESCE(w.total_earned_cents,0),COALESCE(w.total_redeemed_cents,0),
		COALESCE(w.total_checkin_days,0),COALESCE(w.cycle_id,0),COALESCE(w.cycle_day,0),w.last_checkin_date,
		COALESCE(w.daily_low_count,0),COALESCE(w.eligible_spend,0)::numeric(20,8)::text,
		COALESCE(w.draws_used,0),COALESCE(w.wallet_version,0),COALESCE(w.welfare_balance_version,0),p.enabled,p.launch_at
		FROM users u LEFT JOIN welfare_wallets w ON w.user_id=u.id CROSS JOIN welfare_programs p
		WHERE u.id=$1 AND u.deleted_at IS NULL AND p.id=1`, userID).Scan(
		&w.UserID, &state.AccountBalance, &w.BalanceCents, &w.TotalEarnedCents, &w.TotalRedeemedCents,
		&w.TotalCheckinDays, &w.CycleID, &w.CycleDay, &last, &w.DailyLowCount, &w.EligibleSpend, &w.DrawsUsed,
		&w.WalletVersion, &w.WelfareBalanceVersion, &state.Program.Enabled, &launch)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	if last.Valid {
		w.LastCheckinDate = last.Time.Format("2006-01-02")
	}
	if launch.Valid {
		state.Program.LaunchAt = &launch.Time
	}
	state.Program.RulesVersion = 2
	return state, nil
}

func (r *welfareRepository) Mutate(ctx context.Context, userID int64, kind, key, fingerprint string, apply func(*service.WelfareState) (*service.WelfareMutation, error)) (*service.WelfareOperation, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	state, err := welfareLockedState(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	var storedFingerprint string
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT request_fingerprint,result FROM welfare_operations WHERE user_id=$1 AND type=$2 AND idempotency_key=$3`, userID, kind, key).Scan(&storedFingerprint, &raw)
	if err == nil {
		if storedFingerprint != fingerprint {
			return nil, service.ErrWelfareIdempotencyConflict
		}
		var result service.WelfareOperation
		if err = json.Unmarshal(raw, &result); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return &result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	mutation, err := apply(state)
	if err != nil {
		return nil, err
	}
	if mutation == nil || mutation.Result == nil {
		return nil, fmt.Errorf("empty welfare mutation")
	}
	result := mutation.Result
	result.OperationID = uuid.NewString()
	raw, err = json.Marshal(result)
	if err != nil {
		return nil, err
	}
	audit := []byte(`{}`)
	if mutation.PrivateAudit != nil {
		audit, err = json.Marshal(mutation.PrivateAudit)
		if err != nil {
			return nil, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO welfare_operations(id,user_id,type,idempotency_key,request_fingerprint,result,private_audit) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb)`, result.OperationID, userID, kind, key, fingerprint, string(raw), string(audit))
	if err != nil {
		return nil, err
	}
	w := &state.Wallet
	_, err = tx.ExecContext(ctx, `UPDATE welfare_wallets SET balance_cents=$2,total_earned_cents=$3,total_redeemed_cents=$4,total_checkin_days=$5,cycle_id=$6,cycle_day=$7,last_checkin_date=NULLIF($8,'')::date,daily_low_count=$9,draws_used=$10,wallet_version=$11,welfare_balance_version=$12,updated_at=NOW() WHERE user_id=$1`, userID, w.BalanceCents, w.TotalEarnedCents, w.TotalRedeemedCents, w.TotalCheckinDays, w.CycleID, w.CycleDay, w.LastCheckinDate, w.DailyLowCount, w.DrawsUsed, w.WalletVersion, w.WelfareBalanceVersion)
	if err != nil {
		return nil, err
	}
	var daily, streak int64
	for _, entry := range mutation.Entries {
		_, err = tx.ExecContext(ctx, `INSERT INTO welfare_ledger(user_id,operation_id,type,amount_cents,balance_after_cents,business_date) VALUES($1,$2,$3,$4,$5,$6::date)`, userID, result.OperationID, entry.Type, entry.AmountCents, entry.BalanceAfterCents, mutation.BusinessDate)
		if err != nil {
			return nil, err
		}
		if entry.Type == "daily" {
			daily = entry.AmountCents
		}
		if entry.Type == "streak" {
			streak = entry.AmountCents
		}
	}
	if mutation.Checkin {
		_, err = tx.ExecContext(ctx, `INSERT INTO welfare_checkins(user_id,business_date,operation_id,daily_cents,streak_cents,cycle_id,cycle_day) VALUES($1,$2::date,$3,$4,$5,$6,$7)`, userID, mutation.BusinessDate, result.OperationID, daily, streak, w.CycleID, w.CycleDay)
		if err != nil {
			return nil, err
		}
	}
	if mutation.TransferCents > 0 {
		// The numeric expression preserves the user's existing eight decimal places.
		// This is a transfer, so recharge and affiliate counters are untouched.
		_, err = tx.ExecContext(ctx, `UPDATE users SET balance=balance+$2::numeric/100,updated_at=NOW() WHERE id=$1`, userID, mutation.TransferCents)
		if err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO welfare_balance_outbox(user_id,version) VALUES($1,$2)`, userID, w.WalletVersion)
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *welfareRepository) Operation(ctx context.Context, userID int64, id string) (*service.WelfareOperation, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, service.ErrWelfareOperationNotFound
	}
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT result FROM welfare_operations WHERE user_id=$1 AND id=$2`, userID, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrWelfareOperationNotFound
	}
	if err != nil {
		return nil, err
	}
	var result service.WelfareOperation
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *welfareRepository) Calendar(ctx context.Context, userID int64, month string) ([]service.WelfareCalendarDay, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT business_date,daily_cents FROM welfare_checkins WHERE user_id=$1 AND business_date >= $2::date AND business_date < ($2::date+INTERVAL '1 month') ORDER BY business_date`, userID, month+"-01")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	days := make([]service.WelfareCalendarDay, 0, 31)
	for rows.Next() {
		var date time.Time
		var amount int64
		if err = rows.Scan(&date, &amount); err != nil {
			return nil, err
		}
		days = append(days, service.WelfareCalendarDay{Date: date.Format("2006-01-02"), CheckedIn: true, RewardAmount: welfareCentsString(amount)})
	}
	return days, rows.Err()
}

func welfareCentsString(cents int64) string {
	return decimal.NewFromInt(cents).Shift(-2).StringFixed(2)
}

func (r *welfareRepository) Records(ctx context.Context, userID int64, filter service.WelfareRecordFilter) (*service.WelfareRecords, error) {
	args := []any{userID}
	where := []string{"user_id=$1"}
	if filter.Type != "" {
		args = append(args, filter.Type)
		where = append(where, fmt.Sprintf("type=$%d", len(args)))
	}
	if filter.DateFrom != "" {
		args = append(args, filter.DateFrom)
		where = append(where, fmt.Sprintf("created_at >= ($%d::date::timestamp AT TIME ZONE 'Asia/Shanghai')", len(args)))
	}
	if filter.DateTo != "" {
		args = append(args, filter.DateTo)
		where = append(where, fmt.Sprintf("created_at < (($%d::date+1)::timestamp AT TIME ZONE 'Asia/Shanghai')", len(args)))
	}
	clause := strings.Join(where, " AND ")
	// One read snapshot keeps count and page consistent across concurrent awards.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	result := &service.WelfareRecords{Items: []service.WelfareRecord{}, Page: filter.Page, PageSize: filter.PageSize}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM welfare_ledger WHERE `+clause, args...).Scan(&result.Total); err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM welfare_ledger WHERE user_id=$1 AND type='draw'`, userID).Scan(&result.TotalDraws); err != nil {
		return nil, err
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	query := `SELECT id,operation_id,type,amount_cents,balance_after_cents,business_date,created_at FROM welfare_ledger WHERE ` + clause + fmt.Sprintf(` ORDER BY created_at DESC,id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var record service.WelfareRecord
		var id, amount, balance int64
		var date time.Time
		if err = rows.Scan(&id, &record.OperationID, &record.Type, &amount, &balance, &date, &record.CreatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		record.ID = strconv.FormatInt(id, 10)
		record.Amount = welfareCentsString(amount)
		record.BalanceAfter = welfareCentsString(balance)
		record.BusinessDate = date.Format("2006-01-02")
		switch record.Type {
		case "daily":
			record.Description = "Daily check-in"
		case "streak":
			record.Description = "Check-in milestone"
		case "draw":
			record.Description = "Lottery prize"
		case "redeem":
			record.Description = "Transfer to account balance"
		}
		result.Items = append(result.Items, record)
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
