package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// LoadSubscriptionSupplement reads the exact term without a user/group cache.
// A reused standard subscription ID must never inherit an earlier term's credit.
func (r *userSubscriptionRepository) LoadSubscriptionSupplement(ctx context.Context, sub *service.UserSubscription) error {
	if sub == nil {
		return service.ErrSubscriptionNilInput
	}
	sub.SupplementGrantedUSD, sub.SupplementUsedUSD = 0, 0
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, `SELECT granted_usd,used_usd FROM user_subscription_supplement_balances WHERE subscription_id=$1 AND term_starts_at=$2 AND closed_at IS NULL`, sub.ID, sub.StartsAt)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		if err := rows.Scan(&sub.SupplementGrantedUSD, &sub.SupplementUsedUSD); err != nil {
			return err
		}
	}
	return rows.Err()
}
