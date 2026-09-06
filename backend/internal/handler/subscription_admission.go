package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func revalidateGatewaySubscription(c *gin.Context, billing *service.BillingCacheService) error {
	subscription, _ := middleware.GetSubscriptionFromContext(c)
	return billing.RevalidateSubscription(c.Request.Context(), subscription)
}
