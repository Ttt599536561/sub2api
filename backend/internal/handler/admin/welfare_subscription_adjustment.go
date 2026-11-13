package admin

import (
	"context"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type AdjustWelfareSubscriptionRequest struct {
	MonthlyLimitUSD    *float64 `json:"monthly_limit_usd"`
	RemainingDays      *int     `json:"remaining_days"`
	ExpectedVersion    *int64   `json:"expected_version" binding:"required,gte=0"`
	ExpectedGrantCount *int64   `json:"expected_grant_count" binding:"required,gte=1"`
	Reason             string   `json:"reason" binding:"required"`
}

func (h *SubscriptionHandler) AdjustWelfare(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid subscription ID")
		return
	}
	var req AdjustWelfareSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid adjustment request")
		return
	}
	if strings.TrimSpace(c.GetHeader("Idempotency-Key")) == "" {
		response.BadRequest(c, "Idempotency-Key is required")
		return
	}
	actorID, keyID := getAdminIDFromContext(c), ""
	if principal := adminaccess.FromContext(c.Request.Context()); principal != nil {
		actorID, keyID = principal.CreatorID, principal.KeyID
	}
	payload := struct {
		SubscriptionID int64                            `json:"subscription_id"`
		Body           AdjustWelfareSubscriptionRequest `json:"body"`
	}{id, req}
	executeAdminIdempotentJSONWithResponse(c, "admin.subscriptions.adjust-welfare", payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		sub, err := h.subscriptionService.AdjustWelfareSubscription(ctx, id, &service.AdjustWelfareSubscriptionInput{
			MonthlyLimitUSD: req.MonthlyLimitUSD, RemainingDays: req.RemainingDays, ExpectedVersion: *req.ExpectedVersion,
			ExpectedGrantCount: *req.ExpectedGrantCount, Reason: req.Reason, ActorID: actorID, ActorKeyID: keyID,
		})
		if err != nil {
			return nil, err
		}
		return dto.UserSubscriptionFromServiceAdmin(sub), nil
	}, func(data any) any { return adminSubscriptionResponse(c, data) })
}
