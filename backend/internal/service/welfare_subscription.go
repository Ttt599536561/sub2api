package service

import (
	"context"
	"fmt"
	"math"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

// Reward provenance comes from the assigned invitation ledger, never a mutable
// note, group name, code value, or a purchased subscription's duration.
func isAssignedWelfareSubscriptionCode(ctx context.Context, tx *dbent.Tx, codeID int64) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT EXISTS(SELECT 1 FROM qq_invite_code_pool p JOIN qq_invite_rewards r ON r.pool_id=p.id AND r.record_id=p.record_id WHERE p.redeem_code_id=$1 AND p.record_id IS NOT NULL)`, codeID)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	var found bool
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, err
		}
		return false, fmt.Errorf("welfare code provenance returned no row")
	}
	if err := rows.Scan(&found); err != nil {
		return false, err
	}
	return found, rows.Err()
}

// addWelfareQuota preserves an unlimited optional window. Monthly welfare
// quotas are always finite, so no grant can accidentally become unlimited.
func addWelfareQuota(existing, grant *float64) (*float64, error) {
	if existing == nil || grant == nil {
		return nil, nil
	}
	value := *existing + *grant
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value >= 1e10 {
		return nil, infraerrors.BadRequest("WELFARE_QUOTA_INVALID", "welfare quota exceeds the supported range")
	}
	return &value, nil
}

func validWelfareQuota(value *float64, required bool) bool {
	if value == nil {
		return !required
	}
	return !math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0 && (!required || *value > 0) && *value < 1e10
}

// The caller owns the code -> user lock and the redemption transaction. Paid
// subscriptions are never read or changed by this path. Every expired welfare
// cycle keeps its ID for requests that settle after a new cycle has started.
func (s *RedeemService) grantWelfareSubscription(ctx context.Context, tx *dbent.Tx, userID int64, code *RedeemCode) error {
	if s.subscriptionService == nil || code.GroupID == nil || code.ValidityDays != 30 {
		return infraerrors.BadRequest("WELFARE_SUBSCRIPTION_INVALID", "invalid welfare monthly subscription")
	}
	group, err := s.subscriptionService.groupRepo.GetByID(ctx, *code.GroupID)
	if err != nil {
		return err
	}
	if group == nil || !group.IsActive() || !group.IsSubscriptionType() ||
		!validWelfareQuota(group.DailyLimitUSD, false) || !validWelfareQuota(group.WeeklyLimitUSD, false) || !validWelfareQuota(group.MonthlyLimitUSD, true) {
		return infraerrors.BadRequest("WELFARE_QUOTA_INVALID", "welfare monthly passes require a positive monthly quota")
	}
	// Zero has the same unlimited meaning as nil for optional group windows.
	// Normalize on a private copy, without changing a shared group object.
	quotaGroup := *group
	group = &quotaGroup
	if group.DailyLimitUSD != nil && *group.DailyLimitUSD == 0 {
		group.DailyLimitUSD = nil
	}
	if group.WeeklyLimitUSD != nil && *group.WeeklyLimitUSD == 0 {
		group.WeeklyLimitUSD = nil
	}
	now := time.Now()
	existing, err := tx.UserSubscription.Query().Where(
		usersubscription.UserIDEQ(userID), usersubscription.GroupIDEQ(*code.GroupID),
		usersubscription.EntitlementKindEQ(SubscriptionEntitlementWelfare),
		usersubscription.WelfareCycleClosedEQ(false),
	).ForUpdate().Only(ctx)
	if err != nil && !dbent.IsNotFound(err) {
		return err
	}
	if existing != nil && !existing.ExpiresAt.After(now) {
		if _, err := tx.UserSubscription.UpdateOneID(existing.ID).SetWelfareCycleClosed(true).SetStatus(SubscriptionStatusExpired).Save(ctx); err != nil {
			return err
		}
		existing = nil
	}
	var subscriptionID int64
	if existing == nil {
		dailyStart := timezone.StartOfDay(now)
		created, err := tx.UserSubscription.Create().SetUserID(userID).SetGroupID(*code.GroupID).
			SetEntitlementKind(SubscriptionEntitlementWelfare).SetWelfareGrantCount(1).
			SetNillableWelfareDailyLimitUsd(group.DailyLimitUSD).SetNillableWelfareWeeklyLimitUsd(group.WeeklyLimitUSD).
			SetNillableWelfareMonthlyLimitUsd(group.MonthlyLimitUSD).
			SetStartsAt(now).SetExpiresAt(now.AddDate(0, 0, 30)).SetStatus(SubscriptionStatusActive).
			SetDailyWindowStart(dailyStart).SetWeeklyWindowStart(now).SetMonthlyWindowStart(now).
			SetNotes(fmt.Sprintf("QQ邀请福利额度，兑换码 #%d", code.ID)).Save(ctx)
		if err != nil {
			return err
		}
		subscriptionID = created.ID
	} else {
		if existing.Status != SubscriptionStatusActive {
			return ErrSubscriptionInvalid
		}
		daily, err := addWelfareQuota(existing.WelfareDailyLimitUsd, group.DailyLimitUSD)
		if err != nil {
			return err
		}
		weekly, err := addWelfareQuota(existing.WelfareWeeklyLimitUsd, group.WeeklyLimitUSD)
		if err != nil {
			return err
		}
		monthly, err := addWelfareQuota(existing.WelfareMonthlyLimitUsd, group.MonthlyLimitUSD)
		if err != nil || monthly == nil {
			return infraerrors.BadRequest("WELFARE_QUOTA_INVALID", "invalid cumulative welfare monthly quota")
		}
		update := tx.UserSubscription.UpdateOneID(existing.ID).AddWelfareGrantCount(1).SetWelfareMonthlyLimitUsd(*monthly)
		if daily == nil {
			update.ClearWelfareDailyLimitUsd()
		} else {
			update.SetWelfareDailyLimitUsd(*daily)
		}
		if weekly == nil {
			update.ClearWelfareWeeklyLimitUsd()
		} else {
			update.SetWelfareWeeklyLimitUsd(*weekly)
		}
		// Deliberately leave dates, windows, and already consumed quota untouched.
		if _, err := update.Save(ctx); err != nil {
			return err
		}
		subscriptionID = existing.ID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO user_welfare_subscription_grants(redeem_code_id,subscription_id,daily_limit_usd,weekly_limit_usd,monthly_limit_usd,granted_at) VALUES($1,$2,$3,$4,$5,$6)`, code.ID, subscriptionID, group.DailyLimitUSD, group.WeeklyLimitUSD, group.MonthlyLimitUSD, now)
	return err
}
