//go:build unit

package handler

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestReview2QueuedUsageRetainsOriginalPublicModelAndPrice(t *testing.T) {
	for _, recycled := range []bool{false, true} {
		name := "unchanged_context"
		if recycled {
			name = "recycled_context"
		}
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Default.RateMultiplier = 1
			logs, billing := &review2AudioLogs{}, &review2AudioBilling{}
			prices := service.NewBillingService(cfg, nil)
			resolver := service.NewModelPricingResolver(nil, prices)
			gateway := service.NewGatewayService(nil, nil, logs, billing, &review2AudioUser{}, nil, nil, nil, cfg, nil, nil, prices, nil, nil, nil, nil,
				&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, resolver, nil, nil, nil)
			pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
				WorkerCount: 1, QueueSize: 8, TaskTimeout: 10 * time.Second, OverflowPolicy: config.UsageRecordOverflowPolicySync,
			})
			t.Cleanup(pool.Stop)
			block, started := make(chan struct{}), make(chan struct{})
			var unblock sync.Once
			defer unblock.Do(func() { close(block) })
			require.Equal(t, service.UsageRecordSubmitModeEnqueued, pool.Submit(func(context.Context) { close(started); <-block }))
			<-started
			h := &GatewayHandler{gatewayService: gateway, usageRecordWorkerPool: pool}
			groupID, paid, free := int64(24), 10.0, 0.0
			key := &service.APIKey{ID: 20, UserID: 10, GroupID: &groupID, User: &service.User{ID: 10, Balance: 100},
				Group: &service.Group{ID: groupID, Platform: service.PlatformComposite, Status: service.StatusActive, RateMultiplier: 1,
					ModelPricing: []service.ChannelModelPricing{
						{Models: []string{"review-paid"}, BillingMode: service.BillingModeToken, InputPrice: &paid, OutputPrice: &paid},
						{Models: []string{"review-free"}, BillingMode: service.BillingModeToken, InputPrice: &free, OutputPrice: &free},
					}}}
			account := &service.Account{ID: 1, Type: service.AccountTypeAPIKey, Platform: service.PlatformTypeSafe}
			originalCtx := service.WithCompositeRouteDecision(context.Background(), service.CompositeRouteDecision{
				Matched: true, PublicModel: "review-paid", UpstreamModel: "system-1", TargetPlatform: service.PlatformTypeSafe,
			})
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/system-one", nil).WithContext(originalCtx)
			result := &service.SystemOneForwardResult{ForwardResult: service.ForwardResult{
				RequestID: "review-original-request", Model: "system-1", UpstreamModel: "system-1", Usage: service.ClaudeUsage{InputTokens: 100},
			}}
			// This is the production post-forward submission path, including its
			// real worker queue, RecordUsage price resolution and billing command.
			h.recordSystemOneUsage(c, key, account, nil, service.ChannelMappingResult{BillingModelSource: service.BillingModelSourceRequested},
				"system-1", []byte(`{"model":"review-paid"}`), result, key.UserID, time.Now())
			require.EqualValues(t, 1, pool.Stats().WaitingTasks)
			if recycled {
				nextCtx := service.WithCompositeRouteDecision(context.Background(), service.CompositeRouteDecision{
					Matched: true, PublicModel: "review-free", UpstreamModel: "system-1", TargetPlatform: service.PlatformTypeSafe,
				})
				// Gin's pool reassigns Request when the handler has returned.
				// Do so before releasing the blocked worker, without sleep/races.
				c.Request = httptest.NewRequest("POST", "/v1/system-one", nil).WithContext(nextCtx)
			}
			unblock.Do(func() { close(block) })
			pool.Stop()
			logs.mu.Lock()
			defer logs.mu.Unlock()
			billing.mu.Lock()
			defer billing.mu.Unlock()
			require.Len(t, logs.rows, 1)
			require.Len(t, billing.commands, 1)
			t.Logf("requested_model=%s balance_cost=%g", logs.rows[0].RequestedModel, billing.commands[0].BalanceCost)
			require.Positive(t, billing.commands[0].BalanceCost, "another request's free public model must not make this paid request free")
			require.Equal(t, "review-paid", logs.rows[0].RequestedModel)
			require.Equal(t, "system-1", logs.rows[0].Model)
		})
	}
}
