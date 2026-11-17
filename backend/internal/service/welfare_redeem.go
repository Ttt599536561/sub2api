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
	SubscriptionEffectAddQuota       = "add_quota"
	SubscriptionEffectExtendDuration = "extend_duration"
)

var (
	ErrWelfareSupplementInvalid         = infraerrors.BadRequest("WELFARE_SUPPLEMENT_INVALID", "invalid welfare supplement; choose one positive quota amount or duration")
	ErrWelfareSupplementCampaignInvalid = infraerrors.Conflict("WELFARE_SUPPLEMENT_CAMPAIGN_INVALID", "welfare campaign or its subscription group is unavailable or changed")
	ErrWelfareSupplementNoActiveCycle   = infraerrors.Conflict("WELFARE_SUPPLEMENT_NO_ACTIVE_CYCLE", "an active, unexpired invitation welfare subscription is required; code remains unused")
	ErrWelfareSupplementLimitExceeded   = infraerrors.BadRequest("WELFARE_SUPPLEMENT_LIMIT_EXCEEDED", "supplement would exceed the supported quota or expiry limit")
)

type WelfareSupplementResult struct {
	SubscriptionID  int64     `json:"subscription_id"`
	MonthlyLimitUSD float64   `json:"monthly_limit_usd"`
	MonthlyUsageUSD float64   `json:"monthly_usage_usd"`
	RemainingUSD    float64   `json:"remaining_usd"`
	ExpiresAt       time.Time `json:"expires_at"`
}

type WelfareRedeemCampaign struct {
	ID                  int64  `json:"id"`
	Name                string `json:"name"`
	Version             int64  `json:"version"`
	SubscriptionGroupID int64  `json:"subscription_group_id"`
	GroupName           string `json:"group_name"`
}

func validateWelfareSupplement(input *GenerateRedeemCodesInput) error {
	if input != nil && (input.SubscriptionEffect == SubscriptionEffectAddBalance || input.SubscriptionEffect == SubscriptionEffectExtendSubscription) {
		return validateSubscriptionSupplement(input)
	}
	if input == nil || input.Type != RedeemTypeSubscription || input.Count < 1 || input.Count > 100 ||
		input.WelfareCampaignID == nil || *input.WelfareCampaignID <= 0 || input.Value != 0 {
		return ErrWelfareSupplementInvalid
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(time.Now()) {
		return ErrRedeemCodeExpired
	}
	switch input.SubscriptionEffect {
	case SubscriptionEffectAddQuota:
		if input.QuotaAmountUSD == nil || input.ValidityDays != 0 {
			return ErrWelfareSupplementInvalid
		}
		v := *input.QuotaAmountUSD
		if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v >= 1e10 || v != math.Round(v*100)/100 {
			return ErrWelfareSupplementInvalid
		}
	case SubscriptionEffectExtendDuration:
		if input.QuotaAmountUSD != nil || input.ValidityDays < 1 || input.ValidityDays > MaxValidityDays {
			return ErrWelfareSupplementInvalid
		}
	default:
		return ErrWelfareSupplementInvalid
	}
	return nil
}

// Lock order matches QQ campaign writes: campaign -> group. Group lifecycle
// guards hold only the group lock and read references without locking them.
func lockWelfareRedeemCampaign(ctx context.Context, tx *dbent.Tx, id int64) (*WelfareRedeemCampaign, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,version,payload->>'name',COALESCE((payload->>'subscription_group_id')::bigint,0) FROM qq_invite_campaigns WHERE id=$1 FOR SHARE`, id)
	if err != nil {
		return nil, err
	}
	c := &WelfareRedeemCampaign{}
	if !rows.Next() {
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
		return nil, ErrWelfareSupplementCampaignInvalid
	}
	err = rows.Scan(&c.ID, &c.Version, &c.Name, &c.SubscriptionGroupID)
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT name FROM groups WHERE id=$1 AND deleted_at IS NULL AND status='active' AND subscription_type='subscription' FOR SHARE`, c.SubscriptionGroupID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, ErrWelfareSupplementCampaignInvalid
	}
	if err := rows.Scan(&c.GroupName); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *RedeemService) ListWelfareRedeemCampaigns(ctx context.Context) ([]WelfareRedeemCampaign, error) {
	if s == nil || s.entClient == nil {
		return nil, ErrWelfareSupplementCampaignInvalid
	}
	rows, err := s.entClient.QueryContext(ctx, `SELECT c.id,COALESCE(c.payload->>'name',''),c.version,g.id,g.name
 FROM qq_invite_campaigns c JOIN groups g ON g.id=(c.payload->>'subscription_group_id')::bigint
 WHERE g.deleted_at IS NULL AND g.status='active' AND g.subscription_type='subscription' ORDER BY c.id DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]WelfareRedeemCampaign, 0)
	for rows.Next() {
		var c WelfareRedeemCampaign
		if err := rows.Scan(&c.ID, &c.Name, &c.Version, &c.SubscriptionGroupID, &c.GroupName); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func generateWelfareSupplementCodes(ctx context.Context, client *dbent.Client, repo RedeemCodeRepository, input *GenerateRedeemCodesInput) ([]RedeemCode, error) {
	if err := validateWelfareSupplement(input); err != nil {
		return nil, err
	}
	if client == nil || repo == nil {
		return nil, ErrWelfareSupplementInvalid
	}
	tx, err := client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	campaign, err := lockWelfareRedeemCampaign(txCtx, tx, *input.WelfareCampaignID)
	if err != nil {
		return nil, err
	}
	if input.GroupID != nil && *input.GroupID != campaign.SubscriptionGroupID {
		return nil, ErrWelfareSupplementCampaignInvalid
	}
	codes := make([]RedeemCode, 0, input.Count)
	for i := 0; i < input.Count; i++ {
		text, err := GenerateRedeemCode()
		if err != nil {
			return nil, err
		}
		code := RedeemCode{Code: text, Type: RedeemTypeSubscription, Status: StatusUnused, GroupID: &campaign.SubscriptionGroupID,
			SubscriptionEffect: normalizedSubscriptionSupplementEffect(input.SubscriptionEffect), WelfareCampaignID: &campaign.ID, WelfareCampaignVersion: &campaign.Version,
			WelfareCampaignName: campaign.Name, QuotaAmountUSD: input.QuotaAmountUSD, ValidityDays: input.ValidityDays, ExpiresAt: input.ExpiresAt}
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

// The caller already owns code -> user locks. This path never creates a cycle,
// never touches QQ grants, and performs monetary arithmetic entirely in NUMERIC.
func (s *RedeemService) applyWelfareSupplement(ctx context.Context, tx *dbent.Tx, userID int64, code *RedeemCode) error {
	input := &GenerateRedeemCodesInput{Count: 1, Type: code.Type, SubscriptionEffect: code.SubscriptionEffect, WelfareCampaignID: code.WelfareCampaignID, QuotaAmountUSD: code.QuotaAmountUSD, ValidityDays: code.ValidityDays}
	if err := validateWelfareSupplement(input); err != nil {
		return err
	}
	if code.GroupID == nil || code.WelfareCampaignVersion == nil {
		return ErrWelfareSupplementInvalid
	}
	campaign, err := lockWelfareRedeemCampaign(ctx, tx, *code.WelfareCampaignID)
	if err != nil {
		return err
	}
	if campaign.SubscriptionGroupID != *code.GroupID {
		return ErrWelfareSupplementCampaignInvalid
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,welfare_monthly_limit_usd::text,expires_at,daily_reset_version
 FROM user_subscriptions WHERE user_id=$1 AND group_id=$2 AND entitlement_kind='welfare'
 AND deleted_at IS NULL AND NOT welfare_cycle_closed AND status='active' AND expires_at>clock_timestamp() FOR UPDATE`, userID, *code.GroupID)
	if err != nil {
		return err
	}
	var id, version int64
	var beforeQuota string
	var beforeExpiry time.Time
	if !rows.Next() {
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		return ErrWelfareSupplementNoActiveCycle
	}
	err = rows.Scan(&id, &beforeQuota, &beforeExpiry, &version)
	_ = rows.Close()
	if err != nil {
		return err
	}
	afterExpiry := beforeExpiry
	amount := "0"
	if code.SubscriptionEffect == SubscriptionEffectAddQuota {
		amount = strconv.FormatFloat(*code.QuotaAmountUSD, 'f', 2, 64)
	} else {
		afterExpiry = beforeExpiry.AddDate(0, 0, code.ValidityDays)
		if afterExpiry.After(MaxExpiresAt) {
			return ErrWelfareSupplementLimitExceeded
		}
	}
	// Check expiry again after acquiring the row lock; a concurrent administrator
	// or a long lock wait must not revive an already expired entitlement.
	result, err := tx.ExecContext(ctx, `UPDATE user_subscriptions SET welfare_monthly_limit_usd=welfare_monthly_limit_usd+$2::numeric,
 expires_at=$3,daily_reset_version=daily_reset_version+1,updated_at=now()
 WHERE id=$1 AND status='active' AND NOT welfare_cycle_closed AND deleted_at IS NULL AND expires_at>clock_timestamp()
 AND welfare_monthly_limit_usd+$2::numeric < 10000000000`, id, amount, afterExpiry)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrWelfareSupplementLimitExceeded
	}
	if code.SubscriptionEffect == SubscriptionEffectAddQuota {
		// The subscription row remains locked through both the projection and
		// immutable receipt writes. A failed receipt rolls back all three.
		_, err = tx.ExecContext(ctx, `INSERT INTO user_welfare_subscription_supplement_quota(subscription_id,protected_quota_usd)
 VALUES($1,$2::numeric) ON CONFLICT(subscription_id) DO UPDATE
 SET protected_quota_usd=user_welfare_subscription_supplement_quota.protected_quota_usd+EXCLUDED.protected_quota_usd`, id, amount)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO user_welfare_subscription_supplements
 (redeem_code_id,subscription_id,user_id,group_id,welfare_campaign_id,welfare_campaign_version,subscription_effect,quota_amount_usd,validity_days,
 before_monthly_limit_usd,after_monthly_limit_usd,monthly_usage_usd,before_expires_at,after_expires_at,before_version,after_version)
 SELECT $1,id,user_id,group_id,$3,$4,$5,$6,$7,$8::numeric,welfare_monthly_limit_usd,monthly_usage_usd,$9,expires_at,$10,daily_reset_version
 FROM user_subscriptions WHERE id=$2`, code.ID, id, *code.WelfareCampaignID, *code.WelfareCampaignVersion, code.SubscriptionEffect, code.QuotaAmountUSD, code.ValidityDays, beforeQuota, beforeExpiry, version)
	return err
}
