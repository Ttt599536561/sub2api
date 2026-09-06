//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type queuedSubscriptionAdmission struct {
	err    error
	checks int
}

func (s *queuedSubscriptionAdmission) GetSubscriptionForAdmission(context.Context, int64, int64) (*service.UserSubscription, *service.Group, error) {
	s.checks++
	return nil, nil, s.err
}

func (*queuedSubscriptionAdmission) TriggerAutoDailyReset(context.Context, int64) error { return nil }

func TestAccountSlotRejectsSubscriptionExpiredWhileQueued(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, acquired := range []bool{false, true} {
		t.Run(map[bool]string{false: "wait plan", true: "scheduler acquired"}[acquired], func(t *testing.T) {
			cache := &profitCountingConcurrencyCache{}
			billing := &service.BillingCacheService{}
			admission := &queuedSubscriptionAdmission{err: service.ErrSubscriptionExpired}
			billing.SetSubscriptionService(admission)
			h := &OpenAIGatewayHandler{
				gatewayService: &service.OpenAIGatewayService{}, billingCacheService: billing,
				concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatClaude, 0),
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Set(string(middleware.ContextKeySubscription), &service.UserSubscription{ID: 3, UserID: 1, GroupID: 2})
			var schedulerReleases atomic.Int64
			selection := &service.AccountSelectionResult{
				Account: profitSlotTestAccount(1, 0.3), Acquired: acquired,
				WaitPlan: &service.AccountWaitPlan{AccountID: 1, MaxConcurrency: 2, MaxWaiting: 2, Timeout: time.Second},
			}
			if acquired {
				selection.ReleaseFunc = func() { schedulerReleases.Add(1) }
			}
			streamStarted := false
			release, result := h.acquireResponsesAccountSlot(c, nil, "", selection, false, &streamStarted, zap.NewNop())
			require.Equal(t, openAISlotAcquireFailed, result)
			require.Nil(t, release)
			require.Equal(t, 1, admission.checks)
			require.Equal(t, http.StatusForbidden, w.Code)
			require.Equal(t, int64(1), schedulerReleases.Load()+cache.accountReleases.Load())
		})
	}
}

func TestGatewaySubscriptionRevalidatesEachTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Set(string(middleware.ContextKeySubscription), &service.UserSubscription{ID: 3, UserID: 1, GroupID: 2})
	billing := &service.BillingCacheService{}
	admission := &queuedSubscriptionAdmission{err: service.ErrSubscriptionExpired}
	billing.SetSubscriptionService(admission)
	for i := 0; i < 2; i++ {
		require.ErrorIs(t, revalidateGatewaySubscription(c, billing), service.ErrSubscriptionExpired)
	}
	require.Equal(t, 2, admission.checks)
}
