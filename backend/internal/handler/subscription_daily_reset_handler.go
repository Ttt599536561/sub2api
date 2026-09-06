package handler

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type dailyResetResponse struct {
	OperationID     string                `json:"operation_id"`
	Replayed        bool                  `json:"replayed"`
	Subscription    *dto.UserSubscription `json:"subscription"`
	ResetPerformed  bool                  `json:"reset_performed"`
	PreferenceSaved bool                  `json:"preference_saved,omitempty"`
	CheckError      string                `json:"check_error,omitempty"`
}

func resetResponse(out *service.DailyResetOutcome) *dailyResetResponse {
	return &dailyResetResponse{OperationID: out.OperationID, Replayed: out.Replayed,
		Subscription: dto.UserSubscriptionFromService(out.Subscription), ResetPerformed: out.ResetPerformed,
		PreferenceSaved: out.PreferenceSaved, CheckError: out.CheckError}
}

func resetRequestIdentity(c *gin.Context) (int64, int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return 0, 0, false
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid subscription ID")
		return 0, 0, false
	}
	return subject.UserID, id, true
}

func (h *SubscriptionHandler) ResetDaily(c *gin.Context) {
	userID, id, ok := resetRequestIdentity(c)
	if !ok {
		return
	}
	var req struct {
		ExpectedVersion *int64 `json:"expected_version" binding:"required,gte=0"`
		ExpectedDate    string `json:"expected_date" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "A version and observed date are required")
		return
	}
	out, err := h.subscriptionService.ResetSubscriptionDaily(c.Request.Context(), userID, id, *req.ExpectedVersion, req.ExpectedDate, c.GetHeader("Idempotency-Key"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, resetResponse(out))
}

func (h *SubscriptionHandler) SetAutoDailyReset(c *gin.Context) {
	userID, id, ok := resetRequestIdentity(c)
	if !ok {
		return
	}
	var req struct {
		ExpectedVersion *int64 `json:"expected_version" binding:"required,gte=0"`
		Enabled         *bool  `json:"enabled" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "A version and enabled value are required")
		return
	}
	out, err := h.subscriptionService.SetAutoDailyReset(c.Request.Context(), userID, id, *req.ExpectedVersion, *req.Enabled)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, resetResponse(out))
}

func (h *SubscriptionHandler) GetDailyResetState(c *gin.Context) {
	userID, id, ok := resetRequestIdentity(c)
	if !ok {
		return
	}
	sub, err := h.subscriptionService.GetDailyResetState(c.Request.Context(), userID, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.UserSubscriptionFromService(sub))
}

func (h *SubscriptionHandler) GetDailyResetOperation(c *gin.Context) {
	userID, id, ok := resetRequestIdentity(c)
	if !ok {
		return
	}
	out, err := h.subscriptionService.GetDailyResetOperation(c.Request.Context(), userID, id, c.Param("operationId"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, resetResponse(out))
}
