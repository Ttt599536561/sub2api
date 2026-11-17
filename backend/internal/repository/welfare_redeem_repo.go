package repository

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"strings"
)

func (r *redeemCodeRepository) loadWelfareSupplementSlice(ctx context.Context, codes []service.RedeemCode) error {
	refs := make([]*service.RedeemCode, len(codes))
	for i := range codes {
		refs[i] = &codes[i]
	}
	return r.loadWelfareSupplementResults(ctx, refs)
}

func (r *redeemCodeRepository) loadWelfareSupplementResults(ctx context.Context, codes []*service.RedeemCode) error {
	byID := make(map[int64]*service.RedeemCode)
	args := make([]any, 0)
	params := make([]string, 0)
	for _, c := range codes {
		if c.SubscriptionEffect != "" && c.Status == service.StatusUsed {
			byID[c.ID] = c
			args = append(args, c.ID)
			params = append(params, fmt.Sprintf("$%d", len(args)))
		}
	}
	if len(args) == 0 {
		return nil
	}
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, `SELECT redeem_code_id,subscription_id,after_monthly_limit_usd,monthly_usage_usd,
 GREATEST(0,after_monthly_limit_usd-monthly_usage_usd),after_expires_at FROM user_welfare_subscription_supplements WHERE redeem_code_id IN (`+strings.Join(params, ",")+`)`, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var result service.WelfareSupplementResult
		if err := rows.Scan(&id, &result.SubscriptionID, &result.MonthlyLimitUSD, &result.MonthlyUsageUSD, &result.RemainingUSD, &result.ExpiresAt); err != nil {
			return err
		}
		byID[id].WelfareResult = &result
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	return r.loadSubscriptionSupplementResults(ctx, byID, args, params)
}

func (r *redeemCodeRepository) loadSubscriptionSupplementResults(ctx context.Context, byID map[int64]*service.RedeemCode, args []any, params []string) error {
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, `SELECT redeem_code_id,subscription_id,entitlement_kind,group_id,subscription_effect,amount_usd,validity_days,
 after_granted_usd,used_usd,GREATEST(0,after_granted_usd-used_usd),after_expires_at
 FROM user_subscription_supplements WHERE redeem_code_id IN (`+strings.Join(params, ",")+`)`, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var result service.SubscriptionSupplementResult
		if err := rows.Scan(&id, &result.SubscriptionID, &result.EntitlementKind, &result.GroupID, &result.SubscriptionEffect, &result.AmountUSD, &result.ValidityDays,
			&result.SupplementGrantedUSD, &result.SupplementUsedUSD, &result.SupplementRemainingUSD, &result.ExpiresAt); err != nil {
			return err
		}
		byID[id].SubscriptionSupplementResult = &result
	}
	return rows.Err()
}
