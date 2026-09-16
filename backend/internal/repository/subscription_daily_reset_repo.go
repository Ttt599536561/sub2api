package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type subscriptionDailyResetRepository struct{ db *sql.DB }

func NewSubscriptionDailyResetRepository(_ *dbent.Client, db *sql.DB) service.SubscriptionDailyResetRepository {
	return &subscriptionDailyResetRepository{db: db}
}

func (r *subscriptionDailyResetRepository) beginLocked(ctx context.Context, userID, id int64) (*sql.Tx, *service.UserSubscription, error) {
	for attempt := 0; ; attempt++ {
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, nil, err
		}
		sub, err := r.lock(ctx, tx, userID, id)
		if err == nil {
			return tx, sub, nil
		}
		_ = tx.Rollback()
		var pgErr *pq.Error
		if attempt >= 99 || !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
			return nil, nil, err
		}
		// Release earlier locks before retrying so billing and user deletion can finish.
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// Every entry locks the user, group, then subscription so status and configuration
// changes serialize with both preference changes and paid resets.
func (r *subscriptionDailyResetRepository) lock(ctx context.Context, tx *sql.Tx, userID, id int64) (*service.UserSubscription, error) {
	u := &service.User{ID: userID}
	if err := tx.QueryRowContext(ctx, "SELECT status, deleted_at FROM users WHERE id = $1 FOR SHARE", userID).Scan(&u.Status, &u.DeletedAt); err != nil {
		return nil, resetNotFound(err)
	}
	var groupJSON []byte
	err := tx.QueryRowContext(ctx, `SELECT row_to_json(g)
		FROM groups g WHERE g.id = (SELECT group_id FROM user_subscriptions WHERE id = $1 AND user_id = $2)
		FOR SHARE OF g NOWAIT`, id, userID).Scan(&groupJSON)
	if err != nil {
		return nil, resetNotFound(err)
	}
	var entityGroup dbent.Group
	if err := json.Unmarshal(groupJSON, &entityGroup); err != nil {
		return nil, service.ErrResetInvalidData.WithCause(err)
	}
	g := groupEntityToService(&entityGroup)
	if entityGroup.DeletedAt != nil {
		g.Status = "deleted"
	}
	var subscriptionJSON []byte
	err = tx.QueryRowContext(ctx, `SELECT row_to_json(s)
		FROM user_subscriptions s WHERE id = $1 AND user_id = $2 AND group_id = $3 FOR UPDATE NOWAIT`, id, userID, g.ID).
		Scan(&subscriptionJSON)
	if err != nil {
		return nil, resetNotFound(err)
	}
	var entitySub dbent.UserSubscription
	if err := json.Unmarshal(subscriptionJSON, &entitySub); err != nil {
		return nil, service.ErrResetInvalidData.WithCause(err)
	}
	s := userSubscriptionEntityToService(&entitySub)
	s.User = u
	s.Group = g
	return s, nil
}

func resetNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrSubscriptionNotFound
	}
	return err
}

func (r *subscriptionDailyResetRepository) state(ctx context.Context, tx *sql.Tx, s *service.UserSubscription, maintain bool) (*service.SubscriptionDailyResetState, error) {
	state := &service.SubscriptionDailyResetState{Subscription: s}
	if err := tx.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&state.ServerTime); err != nil {
		return nil, err
	}
	if maintain && s.DeletedAt == nil && s.Status == service.SubscriptionStatusActive && s.ExpiresAt.After(state.ServerTime) && s.MaintainUsageWindowsAt(state.ServerTime) {
		if err := saveResetSubscription(ctx, tx, s, state.ServerTime); err != nil {
			return nil, err
		}
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_daily_reset_events WHERE subscription_id = $1 AND count_date = $2::date`,
		s.ID, resetDate(state.ServerTime)).Scan(&state.TodayCount); err != nil {
		return nil, err
	}
	return state, nil
}

func resetDate(now time.Time) string { return now.In(timezone.Location()).Format("2006-01-02") }

func saveResetSubscription(ctx context.Context, tx *sql.Tx, s *service.UserSubscription, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE user_subscriptions SET daily_usage_usd = $2,
		weekly_usage_usd = $3, monthly_usage_usd = $4, daily_window_start = $5,
		weekly_window_start = $6, monthly_window_start = $7, expires_at = $8,
		daily_reset_version = $9, preserve_calendar_daily_reset = $10, updated_at = $11 WHERE id = $1`,
		s.ID, s.DailyUsageUSD, s.WeeklyUsageUSD, s.MonthlyUsageUSD, s.DailyWindowStart,
		s.WeeklyWindowStart, s.MonthlyWindowStart, s.ExpiresAt, s.DailyResetVersion, s.PreserveCalendarDailyReset, now)
	return err
}

func (r *subscriptionDailyResetRepository) GetState(ctx context.Context, userID, id int64) (*service.SubscriptionDailyResetState, error) {
	tx, s, err := r.beginLocked(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	state, err := r.state(ctx, tx, s, true)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return state, nil
}

func (r *subscriptionDailyResetRepository) Apply(ctx context.Context, cmd *service.SubscriptionDailyResetCommand) (*service.SubscriptionDailyResetResult, error) {
	if cmd == nil || cmd.OperationID == "" || cmd.RequestFingerprint == "" || len(cmd.OperationID) > 128 || len(cmd.RequestFingerprint) > 128 || (cmd.Source != "manual" && cmd.Source != "automatic") {
		return nil, service.ErrResetInvalidData
	}
	tx, s, err := r.beginLocked(ctx, cmd.UserID, cmd.SubscriptionID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	event, err := queryResetEvent(ctx, tx, cmd.UserID, cmd.SubscriptionID, cmd.OperationID)
	if err != nil && !errors.Is(err, service.ErrResetOperationNotFound) {
		return nil, err
	}
	if event != nil {
		if event.RequestFingerprint != cmd.RequestFingerprint || event.Source != cmd.Source || event.BeforeVersion != cmd.ExpectedVersion || event.CountDate != cmd.ObservedDate {
			return nil, service.ErrResetOperationConflict
		}
		state, err := r.state(ctx, tx, s, false)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &service.SubscriptionDailyResetResult{State: state, Event: event, Replayed: true}, nil
	}
	state, err := r.state(ctx, tx, s, true)
	if err != nil {
		return nil, err
	}
	result := &service.SubscriptionDailyResetResult{State: state}
	var rejection error
	if cmd.ExpectedVersion != s.DailyResetVersion || cmd.ObservedDate != resetDate(state.ServerTime) {
		rejection = service.ErrResetStateChanged
	} else {
		rejection = service.ValidateDailyReset(s, state.ServerTime, state.TodayCount, cmd.Source == "automatic")
		if s.User.DeletedAt != nil {
			rejection = service.ErrResetNotAllowed
		}
	}
	if rejection != nil {
		// Natural maintenance remains committed even when the paid reset is refused.
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return result, rejection
	}
	event = &service.SubscriptionDailyResetEvent{
		SubscriptionID: s.ID, UserID: s.UserID, GroupID: s.GroupID, Source: cmd.Source,
		OperationID: cmd.OperationID, RequestFingerprint: cmd.RequestFingerprint,
		BeforeVersion: s.DailyResetVersion, AfterVersion: s.DailyResetVersion + 1,
		Timezone: timezone.Location().String(), CountDate: resetDate(state.ServerTime), DaySequence: state.TodayCount + 1,
		BeforeDailyUsageUSD: s.DailyUsageUSD, DailyLimitUSD: *s.Group.DailyLimitUSD,
		BeforeExpiresAt: s.ExpiresAt, AfterExpiresAt: s.ExpiresAt.Add(-24 * time.Hour), DecidedAt: state.ServerTime,
	}
	s.PreserveCalendarDailyReset = s.PreserveCalendarDailyReset || !s.HasOneTimeDailyQuota()
	s.DailyUsageUSD = 0
	today := timezone.StartOfDay(state.ServerTime)
	s.DailyWindowStart = &today
	s.ExpiresAt = event.AfterExpiresAt
	s.DailyResetVersion = event.AfterVersion
	if err := saveResetSubscription(ctx, tx, s, state.ServerTime); err != nil {
		return nil, err
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO subscription_daily_reset_events
		(subscription_id, user_id, group_id, source, operation_id, request_fingerprint,
		before_version, after_version, timezone, count_date, day_sequence, before_daily_usage_usd,
		daily_limit_usd, before_expires_at, after_expires_at, decided_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::date,$11,$12,$13,$14,$15,$16) RETURNING id`,
		event.SubscriptionID, event.UserID, event.GroupID, event.Source, event.OperationID, event.RequestFingerprint,
		event.BeforeVersion, event.AfterVersion, event.Timezone, event.CountDate, event.DaySequence, event.BeforeDailyUsageUSD,
		event.DailyLimitUSD, event.BeforeExpiresAt, event.AfterExpiresAt, event.DecidedAt).Scan(&event.ID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	state.TodayCount++
	result.Event = event
	return result, nil
}

func (r *subscriptionDailyResetRepository) SetAutomatic(ctx context.Context, userID, id, expectedVersion int64, enabled bool) (*service.SubscriptionDailyResetState, error) {
	tx, s, err := r.beginLocked(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	state, err := r.state(ctx, tx, s, true)
	if err != nil {
		return nil, err
	}
	if s.DailyResetVersion != expectedVersion {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return state, service.ErrResetStateChanged
	}
	// Turning off remains possible even after expiry or a group permission change.
	if enabled && (s.DeletedAt != nil || s.User.DeletedAt != nil || s.User.Status != service.StatusActive || s.Group.Status != service.StatusActive || s.Status != service.SubscriptionStatusActive || state.ServerTime.Before(s.StartsAt) || !s.ExpiresAt.After(state.ServerTime) || !s.Group.AllowSubscriptionDayReset || s.Group.SubscriptionType != service.SubscriptionTypeSubscription || s.Group.DailyLimitUSD == nil || !finitePositiveResetLimit(*s.Group.DailyLimitUSD)) {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return state, service.ErrResetNotAllowed
	}
	if s.AutoDailyResetEnabled != enabled {
		_, err = tx.ExecContext(ctx, `UPDATE user_subscriptions SET auto_daily_reset_enabled = $2,
			daily_reset_version = daily_reset_version + 1, updated_at = $3 WHERE id = $1`, s.ID, enabled, state.ServerTime)
		if err != nil {
			return nil, err
		}
		s.AutoDailyResetEnabled = enabled
		s.DailyResetVersion++
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return state, nil
}

func finitePositiveResetLimit(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }

type resetEventQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func queryResetEvent(ctx context.Context, db resetEventQuerier, userID, id int64, operation string) (*service.SubscriptionDailyResetEvent, error) {
	e := &service.SubscriptionDailyResetEvent{}
	err := db.QueryRowContext(ctx, `SELECT id, subscription_id, user_id, group_id, source, operation_id,
		request_fingerprint, before_version, after_version, timezone, count_date::text, day_sequence,
		before_daily_usage_usd, daily_limit_usd, before_expires_at, after_expires_at, decided_at
		FROM subscription_daily_reset_events WHERE subscription_id = $1 AND user_id = $2 AND operation_id = $3`, id, userID, operation).
		Scan(&e.ID, &e.SubscriptionID, &e.UserID, &e.GroupID, &e.Source, &e.OperationID,
			&e.RequestFingerprint, &e.BeforeVersion, &e.AfterVersion, &e.Timezone, &e.CountDate, &e.DaySequence,
			&e.BeforeDailyUsageUSD, &e.DailyLimitUSD, &e.BeforeExpiresAt, &e.AfterExpiresAt, &e.DecidedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrResetOperationNotFound
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (r *subscriptionDailyResetRepository) GetOperation(ctx context.Context, userID, id int64, operation string) (*service.SubscriptionDailyResetEvent, error) {
	return queryResetEvent(ctx, r.db, userID, id, operation)
}

func (r *subscriptionDailyResetRepository) ListAutomaticCandidates(ctx context.Context, afterID int64, limit int) ([]service.SubscriptionDailyResetCandidate, error) {
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("automatic reset scan limit must be between 1 and 1000")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT s.id, s.user_id, s.daily_reset_version
		FROM user_subscriptions s JOIN groups g ON g.id = s.group_id JOIN users u ON u.id = s.user_id
		WHERE s.id > $1 AND s.auto_daily_reset_enabled AND s.deleted_at IS NULL AND s.status = 'active'
		AND s.expires_at > clock_timestamp() + INTERVAL '86400 seconds'
		AND g.deleted_at IS NULL AND g.status = 'active' AND g.allow_subscription_day_reset
		AND g.subscription_type = 'subscription' AND g.daily_limit_usd > 0
		AND g.daily_limit_usd < 'Infinity'::numeric AND s.daily_usage_usd >= g.daily_limit_usd
		AND s.daily_usage_usd < 'Infinity'::numeric AND u.deleted_at IS NULL AND u.status = 'active'
		ORDER BY s.id LIMIT $2`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]service.SubscriptionDailyResetCandidate, 0)
	for rows.Next() {
		var candidate service.SubscriptionDailyResetCandidate
		if err := rows.Scan(&candidate.ID, &candidate.UserID, &candidate.DailyResetVersion); err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	return result, rows.Err()
}
