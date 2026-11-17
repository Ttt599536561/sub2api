//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDeferredVideoCompletionPreservesSubscriptionTerm(t *testing.T) {
	for _, provider := range []string{"grok", "seedance"} {
		t.Run(provider, func(t *testing.T) {
			h, _, _, _ := newGrokMediaSlotHandler(t, false, false)
			c, _ := grokMediaSlotContext(context.Background(), false)
			key, _ := middleware.GetAPIKeyFromContext(c)
			subject, _ := middleware.GetAuthSubjectFromContext(c)
			start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
			binding := service.CaptureGrokVideoBillingBinding(subject.UserID, key.GroupID, &service.UserSubscription{ID: 91, StartsAt: start})
			taskID := "task-original-term"
			if provider == "seedance" {
				taskID = "seedance:" + taskID
			}
			require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(c.Request.Context(), taskID, subject.UserID, key.ID,
				service.GrokVideoPendingBilling{Model: "video", VideoDurationSeconds: 8, SubscriptionBinding: binding}))
			result := &service.OpenAIForwardResult{ResponseID: taskID, VideoCount: 1, Usage: service.OpenAIUsage{OutputTokens: 100}}
			var billed *service.OpenAIForwardResult
			if provider == "seedance" {
				billed = prepareSeedanceCompletionBilling(c.Request.Context(), h, key, subject, taskID, result)
			} else {
				billed = prepareGrokVideoCompletionBilling(c.Request.Context(), h, zap.NewNop(), key, subject, taskID, result)
			}
			require.NotNil(t, billed)
			require.Equal(t, binding, billed.VideoBillingBinding)
			renewed := &service.UserSubscription{ID: 91, StartsAt: start.AddDate(0, 1, 0), SupplementGrantedUSD: 50}
			settlementSubscription, err := billed.VideoBillingBinding.RestoreSubscription(subject.UserID, key.GroupID, renewed)
			require.NoError(t, err)
			require.Equal(t, start, settlementSubscription.StartsAt)
		})
	}
}

func TestDeferredVideoSnapshotSurvivesRedisLoss(t *testing.T) {
	h, _, bindings, _ := newGrokMediaSlotHandler(t, false, false)
	c, _ := grokMediaSlotContext(context.Background(), false)
	key, _ := middleware.GetAPIKeyFromContext(c)
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	binding := service.CaptureGrokVideoBillingBinding(subject.UserID, key.GroupID, &service.UserSubscription{ID: 91, StartsAt: time.Now().Add(-time.Hour)})
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(c.Request.Context(), "task", subject.UserID, key.ID, service.GrokVideoPendingBilling{Model: "video", SubscriptionBinding: binding}))
	bindings.pending = nil
	pending, err := h.gatewayService.LoadGrokVideoPendingBilling(c.Request.Context(), "task", subject.UserID, key.ID)
	require.NoError(t, err)
	require.NotNil(t, pending, "new task identity must survive Redis expiry")
	require.Equal(t, binding.SubscriptionID, pending.SubscriptionBinding.SubscriptionID)
	other, err := h.gatewayService.LoadGrokVideoPendingBilling(c.Request.Context(), "task", subject.UserID+1, key.ID)
	require.NoError(t, err)
	require.Nil(t, other)
}

func TestDeferredVideoCacheLoadErrorDoesNotBillPollingSubscription(t *testing.T) {
	h, _, bindings, _ := newGrokMediaSlotHandler(t, false, false)
	bindings.cacheLoadErr = errors.New("redis unavailable")
	c, _ := grokMediaSlotContext(context.Background(), false)
	key, _ := middleware.GetAPIKeyFromContext(c)
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	result := prepareGrokVideoCompletionBilling(c.Request.Context(), h, zap.NewNop(), key, subject, "task", &service.OpenAIForwardResult{VideoCount: 1, VideoDurationSeconds: 8})
	require.Nil(t, result, "a storage error is not a legacy task with no binding")
}

func TestDeferredVideoCreateDoesNotCommitSuccessBeforeSnapshot(t *testing.T) {
	h, slots, bindings, _ := newGrokMediaSlotHandler(t, false, false)
	bindings.durableStoreErr = errors.New("postgres unavailable")
	c, response := grokMediaSlotContext(context.Background(), true)
	h.GrokVideoGeneration(c)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.NotContains(t, response.Body.String(), `"request_id":"task"`)
	slots.assertReleased(t)
}

func TestDeferredVideoOrphanNewBindingIsNotLegacy(t *testing.T) {
	h, _, bindings, _ := newGrokMediaSlotHandler(t, false, false)
	c, _ := grokMediaSlotContext(context.Background(), false)
	key, _ := middleware.GetAPIKeyFromContext(c)
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	newTask := service.GrokVideoPendingBilling{Model: "video", SubscriptionBinding: service.CaptureGrokVideoBillingBinding(subject.UserID, key.GroupID, nil)}
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(c.Request.Context(), "new", subject.UserID, key.ID, newTask))
	bindings.durable = nil
	_, err := h.gatewayService.LoadGrokVideoPendingBilling(c.Request.Context(), "new", subject.UserID, key.ID)
	require.Error(t, err, "a new bound task missing from PostgreSQL cannot fall back to polling identity")
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(c.Request.Context(), "old", subject.UserID, key.ID, service.GrokVideoPendingBilling{Model: "legacy", VideoDurationSeconds: 8}))
	legacy, err := h.gatewayService.LoadGrokVideoPendingBilling(c.Request.Context(), "old", subject.UserID, key.ID)
	require.NoError(t, err)
	require.NotNil(t, legacy)
	require.Nil(t, legacy.SubscriptionBinding)
}

func TestDeferredVideoOwnerSurvivesRedisExpiryWithinItsScope(t *testing.T) {
	h, _, bindings, _ := newGrokMediaSlotHandler(t, false, false)
	c, _ := grokMediaSlotContext(context.Background(), false)
	key, _ := middleware.GetAPIKeyFromContext(c)
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(c.Request.Context(), "task", subject.UserID, key.ID, service.GrokVideoPendingBilling{
		Model: "video", AccountID: 17, SubscriptionBinding: service.CaptureGrokVideoBillingBinding(subject.UserID, key.GroupID, nil),
	}))
	bindings.owner = 0
	bindings.pending = nil
	owner, err := h.gatewayService.ResolveGrokMediaVideoRequestAccount(c.Request.Context(), key.GroupID, "task", subject.UserID, key.ID)
	require.NoError(t, err)
	require.EqualValues(t, 17, owner)
	owner, _ = h.gatewayService.ResolveGrokMediaVideoRequestAccount(c.Request.Context(), key.GroupID, "task", subject.UserID, key.ID+1)
	require.Zero(t, owner)
}

func TestDeferredVideoUsageContextRetainsPersistentHold(t *testing.T) {
	parent := service.WithSubscriptionSupplementHoldID(context.Background(), "persisted-video-hold")
	got := ""
	task, abandon := wrapUsageRecordTaskContext(parent, func(ctx context.Context) { got = service.SubscriptionSupplementHoldIDFromContext(ctx) })
	defer abandon()
	task(context.Background())
	require.Equal(t, "persisted-video-hold", got)
}

func TestDeferredVideoPostgresReadFailureDoesNotClaim(t *testing.T) {
	h, _, bindings, _ := newGrokMediaSlotHandler(t, false, false)
	bindings.durableLoadErr = errors.New("postgres unavailable")
	c, _ := grokMediaSlotContext(context.Background(), false)
	key, _ := middleware.GetAPIKeyFromContext(c)
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	result := prepareGrokVideoCompletionBilling(c.Request.Context(), h, zap.NewNop(), key, subject, "task", &service.OpenAIForwardResult{VideoCount: 1, VideoDurationSeconds: 8})
	require.Nil(t, result)
	require.Empty(t, bindings.billed)
}

type videoTerminalHoldRepo struct {
	service.UserSubscriptionRepository
	released []string
}

func (r *videoTerminalHoldRepo) ReserveSubscriptionSupplement(context.Context, *service.SubscriptionSupplementReservation) error {
	return nil
}
func (r *videoTerminalHoldRepo) RenewSubscriptionSupplementReservation(context.Context, string, time.Duration) (bool, error) {
	return true, nil
}
func (r *videoTerminalHoldRepo) ReleaseSubscriptionSupplementReservation(_ context.Context, id string) error {
	r.released = append(r.released, id)
	return nil
}
func (r *videoTerminalHoldRepo) TransferSubscriptionSupplementReservation(context.Context, string, string, time.Duration) error {
	return nil
}

func TestDeferredVideoOnlyExplicitTerminalFailureReleasesHold(t *testing.T) {
	for _, status := range []string{"pending", "done", "failed", "expired", "canceled", "cancelled", "delete"} {
		t.Run(status, func(t *testing.T) {
			h, _, _, _ := newGrokMediaSlotHandler(t, false, false)
			repo := &videoTerminalHoldRepo{}
			billing := service.NewBillingCacheService(nil, nil, repo, nil, nil, nil, &config.Config{}, nil)
			defer billing.Stop()
			h.billingCacheService = billing
			c, _ := grokMediaSlotContext(context.Background(), false)
			key, _ := middleware.GetAPIKeyFromContext(c)
			subject, _ := middleware.GetAuthSubjectFromContext(c)
			binding := service.CaptureGrokVideoBillingBinding(subject.UserID, key.GroupID, nil)
			binding.SupplementHoldID = "hold-task"
			require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(c.Request.Context(), "task", subject.UserID, key.ID, service.GrokVideoPendingBilling{Model: "video", SubscriptionBinding: binding}))
			endpoint := service.GrokMediaEndpointVideoStatus
			if status == "delete" {
				endpoint = service.SeedanceEndpointDelete
			}
			releaseGrokVideoTerminalHold(c.Request.Context(), h, zap.NewNop(), key, subject, endpoint, "task", &service.OpenAIForwardResult{UpstreamTerminalEvent: status})
			if status == "pending" || status == "done" {
				require.Empty(t, repo.released)
			} else {
				require.Equal(t, []string{"hold-task"}, repo.released)
			}
		})
	}
}
