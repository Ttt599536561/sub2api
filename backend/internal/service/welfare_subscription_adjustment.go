package service

import (
	"context"
	"database/sql"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

var (
	ErrWelfareAdjustmentInvalid  = infraerrors.BadRequest("WELFARE_ADJUSTMENT_INVALID", "invalid welfare subscription adjustment")
	ErrWelfareAdjustmentConflict = infraerrors.Conflict("WELFARE_ADJUSTMENT_CONFLICT", "subscription changed; reload it before adjusting")
)

type AdjustWelfareSubscriptionInput struct {
	MonthlyLimitUSD    *float64
	RemainingDays      *int
	ExpectedVersion    int64
	ExpectedGrantCount int64
	Reason             string
	ActorID            int64
	ActorKeyID         string
}

func validateWelfareAdjustment(input *AdjustWelfareSubscriptionInput) error {
	if input == nil || (input.MonthlyLimitUSD == nil && input.RemainingDays == nil) || input.ExpectedVersion < 0 || input.ExpectedGrantCount < 1 || input.ActorID < 0 {
		return ErrWelfareAdjustmentInvalid
	}
	reasonLength := utf8.RuneCountInString(strings.TrimSpace(input.Reason))
	if reasonLength < 3 || reasonLength > 500 {
		return ErrWelfareAdjustmentInvalid
	}
	if input.MonthlyLimitUSD != nil {
		value := *input.MonthlyLimitUSD
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value >= 1e10 || value != math.Round(value*100)/100 {
			return ErrWelfareAdjustmentInvalid
		}
	}
	if input.RemainingDays != nil && (*input.RemainingDays < 0 || *input.RemainingDays > MaxValidityDays) {
		return ErrWelfareAdjustmentInvalid
	}
	return nil
}

// AdjustWelfareSubscription changes one current welfare period only. Existing
// consumption, grant receipts, group limits and paid subscriptions are untouched.
// The row lock serializes with redemption; version + grant count reject stale
// administrator forms without treating normal API usage as a write conflict.
func (s *SubscriptionService) AdjustWelfareSubscription(ctx context.Context, id int64, input *AdjustWelfareSubscriptionInput) (*UserSubscription, error) {
	if err := validateWelfareAdjustment(input); err != nil {
		return nil, err
	}
	if id <= 0 || s == nil || s.entClient == nil || s.userSubRepo == nil {
		return nil, ErrWelfareAdjustmentInvalid
	}
	tx, err := s.entClient.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	sub, err := s.userSubRepo.GetByIDForUpdate(txCtx, id)
	if err != nil {
		return nil, err
	}
	if !sub.IsWelfare() || sub.DeletedAt != nil || sub.WelfareCycleClosed || (sub.Status != SubscriptionStatusActive && sub.Status != SubscriptionStatusExpired) {
		return nil, ErrWelfareSubscriptionFixed
	}
	if sub.DailyResetVersion != input.ExpectedVersion || sub.WelfareGrantCount != input.ExpectedGrantCount {
		return nil, ErrWelfareAdjustmentConflict
	}
	if sub.WelfareMonthlyLimitUSD == nil {
		return nil, ErrWelfareAdjustmentInvalid
	}
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	quota, expiry, status := *sub.WelfareMonthlyLimitUSD, sub.ExpiresAt, sub.Status
	if input.MonthlyLimitUSD != nil {
		quota = *input.MonthlyLimitUSD
	}
	if input.RemainingDays != nil {
		expiry = now.Add(time.Duration(*input.RemainingDays) * 24 * time.Hour)
		if expiry.After(MaxExpiresAt) {
			return nil, ErrWelfareAdjustmentInvalid
		}
		status = SubscriptionStatusActive
		if !expiry.After(now) {
			status = SubscriptionStatusExpired
		}
	}
	if quota == *sub.WelfareMonthlyLimitUSD && expiry.Equal(sub.ExpiresAt) && status == sub.Status {
		return nil, ErrWelfareAdjustmentInvalid
	}
	// Never use the generic repository Update: it also writes usage/windows.
	_, err = tx.UserSubscription.UpdateOneID(id).SetWelfareMonthlyLimitUsd(quota).
		SetExpiresAt(expiry).SetStatus(status).AddDailyResetVersion(1).Save(txCtx)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(txCtx, `INSERT INTO user_welfare_subscription_adjustments
	 (subscription_id,user_id,group_id,actor_id,actor_key_id,reason,before_monthly_limit_usd,after_monthly_limit_usd,
	 before_expires_at,after_expires_at,before_status,after_status,before_version,after_version,welfare_grant_count)
	 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		id, sub.UserID, sub.GroupID, input.ActorID, input.ActorKeyID, strings.TrimSpace(input.Reason), *sub.WelfareMonthlyLimitUSD, quota,
		sub.ExpiresAt, expiry, sub.Status, status, sub.DailyResetVersion, sub.DailyResetVersion+1, sub.WelfareGrantCount)
	if err != nil {
		return nil, err
	}
	updated, err := s.userSubRepo.GetByID(txCtx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	// The authoritative welfare path reads the database on every admission.
	// A cache outage after commit must not report the durable adjustment failed.
	if err := s.invalidateSubscriptionCaches(sub.UserID, sub.GroupID); err != nil {
		logger.LegacyPrintf("service.subscription", "invalidate adjusted welfare subscription %d: %v", id, err)
	}
	return updated, nil
}
