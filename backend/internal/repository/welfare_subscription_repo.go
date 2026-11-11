package repository

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.WelfareSubscriptionRepository = (*userSubscriptionRepository)(nil)

// ListWelfareByUserAndGroup includes inactive terms so the caller can distinguish
// an exhausted, suspended, or expired welfare entitlement from a missing one.
// Deleted historical terms are excluded even when the caller bypasses the
// general soft-delete interceptor for another operation.
func (r *userSubscriptionRepository) ListWelfareByUserAndGroup(ctx context.Context, userID, groupID int64) ([]service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	subs, err := client.UserSubscription.Query().
		Where(
			usersubscription.UserIDEQ(userID),
			usersubscription.GroupIDEQ(groupID),
			usersubscription.EntitlementKindEQ(service.SubscriptionEntitlementWelfare),
			usersubscription.WelfareCycleClosedEQ(false),
			usersubscription.DeletedAtIsNil(),
		).
		WithGroup().
		Order(dbent.Asc(usersubscription.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return userSubscriptionEntitiesToService(subs), nil
}
