package service

import (
	"context"
	"database/sql"
	"math"
	"strconv"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	SubscriptionEffectAddBalance         = "add_balance"
	SubscriptionEffectExtendSubscription = "extend_subscription"
)

var (
	ErrSubscriptionSupplementInvalid              = infraerrors.BadRequest("SUBSCRIPTION_SUPPLEMENT_INVALID", "choose a subscription group and one positive supplement amount or duration")
	ErrSubscriptionSupplementGroupInvalid         = infraerrors.Conflict("SUBSCRIPTION_SUPPLEMENT_GROUP_INVALID", "the selected subscription group is unavailable")
	ErrSubscriptionSupplementNoActiveSubscription = infraerrors.Conflict("SUBSCRIPTION_SUPPLEMENT_NO_ACTIVE_SUBSCRIPTION", "an active, unexpired subscription in the selected group is required; code remains unused")
	ErrSubscriptionSupplementLimitExceeded        = infraerrors.BadRequest("SUBSCRIPTION_SUPPLEMENT_LIMIT_EXCEEDED", "supplement would exceed the supported amount or expiry limit")
)

type SubscriptionSupplementResult struct {
	SubscriptionID         int64     `json:"subscription_id"`
	EntitlementKind        string    `json:"entitlement_kind"`
	GroupID                int64     `json:"group_id"`
	SubscriptionEffect     string    `json:"subscription_effect"`
	AmountUSD              float64   `json:"amount_usd"`
	ValidityDays           int       `json:"validity_days"`
	SupplementGrantedUSD   float64   `json:"supplement_granted_usd"`
	SupplementUsedUSD      float64   `json:"supplement_used_usd"`
	SupplementRemainingUSD float64   `json:"supplement_remaining_usd"`
	ExpiresAt              time.Time `json:"expires_at"`
}

func normalizedSubscriptionSupplementEffect(effect string) string {
	switch effect {
	case SubscriptionEffectAddQuota:
		return SubscriptionEffectAddBalance
	case SubscriptionEffectExtendDuration:
		return SubscriptionEffectExtendSubscription
	default:
		return effect
	}
}

func validateSubscriptionSupplement(input *GenerateRedeemCodesInput) error {
	if input == nil || input.Type != RedeemTypeSubscription || input.Count < 1 || input.Count > 100 || input.Value != 0 || input.GroupID == nil || *input.GroupID <= 0 {
		return ErrSubscriptionSupplementInvalid
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(time.Now()) {
		return ErrRedeemCodeExpired
	}
	switch normalizedSubscriptionSupplementEffect(input.SubscriptionEffect) {
	case SubscriptionEffectAddBalance:
		if input.QuotaAmountUSD == nil || input.ValidityDays != 0 {
			return ErrSubscriptionSupplementInvalid
		}
		v := *input.QuotaAmountUSD
		if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v >= 1e10 || v != math.Round(v*100)/100 {
			return ErrSubscriptionSupplementInvalid
		}
	case SubscriptionEffectExtendSubscription:
		if input.QuotaAmountUSD != nil || input.ValidityDays < 1 || input.ValidityDays > MaxValidityDays {
			return ErrSubscriptionSupplementInvalid
		}
	default:
		return ErrSubscriptionSupplementInvalid
	}
	return nil
}

func lockSupplementGroup(ctx context.Context, tx *dbent.Tx, id int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM groups WHERE id=$1 AND deleted_at IS NULL AND status='active' AND subscription_type='subscription' FOR SHARE`, id)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return ErrSubscriptionSupplementGroupInvalid
	}
	return nil
}

func generateSubscriptionSupplementCodes(ctx context.Context, client *dbent.Client, repo RedeemCodeRepository, input *GenerateRedeemCodesInput) ([]RedeemCode, error) {
	if err := validateSubscriptionSupplement(input); err != nil {
		return nil, err
	}
	// New requests select a group directly; historical campaign snapshots are
	// accepted only by the legacy generation endpoint/path.
	if input.WelfareCampaignID != nil || client == nil || repo == nil {
		return nil, ErrSubscriptionSupplementInvalid
	}
	tx, err := client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	if err := lockSupplementGroup(txCtx, tx, *input.GroupID); err != nil {
		return nil, err
	}
	codes := make([]RedeemCode, 0, input.Count)
	for i := 0; i < input.Count; i++ {
		text, err := GenerateRedeemCode()
		if err != nil {
			return nil, err
		}
		code := RedeemCode{Code: text, Type: RedeemTypeSubscription, Status: StatusUnused, GroupID: input.GroupID,
			SubscriptionEffect: input.SubscriptionEffect, QuotaAmountUSD: input.QuotaAmountUSD, ValidityDays: input.ValidityDays, ExpiresAt: input.ExpiresAt}
		if err := repo.Create(txCtx, &code); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return codes, nil
}

// The caller owns code -> user locks. The group and selected subscription remain
// locked through balance, version, receipt and code consumption writes. Legacy
// unused effects intentionally receive the same new behavior; used receipts are
// never replayed or rewritten.
func (s *RedeemService) applySubscriptionSupplement(ctx context.Context, tx *dbent.Tx, userID int64, code *RedeemCode) error {
	input := &GenerateRedeemCodesInput{Count: 1, Type: code.Type, GroupID: code.GroupID, Value: code.Value,
		SubscriptionEffect: code.SubscriptionEffect, QuotaAmountUSD: code.QuotaAmountUSD, ValidityDays: code.ValidityDays}
	if err := validateSubscriptionSupplement(input); err != nil {
		return err
	}
	if err := lockSupplementGroup(ctx, tx, *code.GroupID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,starts_at,expires_at,daily_reset_version
 FROM user_subscriptions WHERE user_id=$1 AND group_id=$2 AND entitlement_kind IN ('welfare','standard')
 AND deleted_at IS NULL AND NOT welfare_cycle_closed AND status='active'
 AND starts_at<=clock_timestamp() AND expires_at>clock_timestamp()
 ORDER BY CASE WHEN entitlement_kind='welfare' THEN 0 ELSE 1 END,id LIMIT 1 FOR UPDATE`, userID, *code.GroupID)
	if err != nil {
		return err
	}
	var id, version int64
	var starts, beforeExpiry time.Time
	if !rows.Next() {
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		return ErrSubscriptionSupplementNoActiveSubscription
	}
	err = rows.Scan(&id, &starts, &beforeExpiry, &version)
	_ = rows.Close()
	if err != nil {
		return err
	}
	effect := normalizedSubscriptionSupplementEffect(code.SubscriptionEffect)
	afterExpiry, amount := beforeExpiry, "0"
	if effect == SubscriptionEffectAddBalance {
		amount = strconv.FormatFloat(*code.QuotaAmountUSD, 'f', 2, 64)
	} else {
		afterExpiry = beforeExpiry.AddDate(0, 0, code.ValidityDays)
		if afterExpiry.After(MaxExpiresAt) {
			return ErrSubscriptionSupplementLimitExceeded
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE user_subscriptions SET expires_at=$2,daily_reset_version=daily_reset_version+1,updated_at=now()
 WHERE id=$1 AND status='active' AND deleted_at IS NULL AND NOT welfare_cycle_closed AND expires_at>clock_timestamp()`, id, afterExpiry)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrSubscriptionSupplementNoActiveSubscription
	}
	if effect == SubscriptionEffectAddBalance {
		result, err = tx.ExecContext(ctx, `INSERT INTO user_subscription_supplement_balances(subscription_id,term_starts_at,granted_usd)
 VALUES($1,$2,$3::numeric) ON CONFLICT(subscription_id,term_starts_at) DO UPDATE
 SET granted_usd=user_subscription_supplement_balances.granted_usd+EXCLUDED.granted_usd,updated_at=now()
 WHERE user_subscription_supplement_balances.granted_usd+EXCLUDED.granted_usd<10000000000`, id, starts, amount)
		if err != nil {
			return err
		}
		n, err = result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrSubscriptionSupplementLimitExceeded
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO user_subscription_supplements
 (redeem_code_id,subscription_id,user_id,group_id,entitlement_kind,term_starts_at,subscription_effect,amount_usd,validity_days,
 before_granted_usd,after_granted_usd,used_usd,before_expires_at,after_expires_at,before_version,after_version)
 SELECT $1,s.id,s.user_id,s.group_id,s.entitlement_kind,s.starts_at,$3,$4::numeric,$5,
 COALESCE(b.granted_usd,0)-$4::numeric,COALESCE(b.granted_usd,0),COALESCE(b.used_usd,0),$6,s.expires_at,$7,s.daily_reset_version
 FROM user_subscriptions s LEFT JOIN user_subscription_supplement_balances b ON b.subscription_id=s.id AND b.term_starts_at=s.starts_at
 WHERE s.id=$2`, code.ID, id, effect, amount, code.ValidityDays, beforeExpiry, version)
	return err
}
