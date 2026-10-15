//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type cachedContentReviewAdmission struct {
	sub    *service.UserSubscription
	group  *service.Group
	checks int
}

func (s *cachedContentReviewAdmission) GetSubscriptionForAdmission(context.Context, int64, int64) (*service.UserSubscription, *service.Group, error) {
	s.checks++
	if s.checks > 1 {
		return nil, nil, service.ErrSubscriptionExpired
	}
	snapshot := *s.sub
	return &snapshot, s.group, nil
}

func (*cachedContentReviewAdmission) TriggerAutoDailyReset(context.Context, int64) error { return nil }

func TestGeminiCachedContentsCreate_RechecksSubscriptionBeforeUpstream(t *testing.T) {
	env := newCachedBillingEnv(t, 500, 900)
	env.group.SubscriptionType = service.SubscriptionTypeSubscription
	sub := &service.UserSubscription{ID: 8, UserID: env.apiKey.UserID, GroupID: env.group.ID,
		Status: service.SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)}
	admission := &cachedContentReviewAdmission{sub: sub, group: env.group}
	env.h.billingCacheService.SetSubscriptionService(admission)
	// A rejected upstream response avoids settlement when running against the
	// broken handler: the terminal subscription check must precede even this POST.
	env.upstream.failures = map[string]cachedBillingUpstreamFailure{
		http.MethodPost: {status: http.StatusBadRequest, body: `{"error":{"message":"unexpected upstream call"}}`},
	}
	rec := env.serve(http.MethodPost, "/v1beta/cachedContents", `{"model":"gemini-2.5-pro"}`, nil, func(c *gin.Context) {
		c.Set(string(middleware.ContextKeySubscription), sub)
		env.h.GeminiCachedContentsCreate(c)
	})
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Equal(t, 2, admission.checks)
	require.Zero(t, env.upstream.methodCalls(http.MethodPost))
	require.Empty(t, env.usage.totals())
}

func TestGeminiCachedContentsAccountSlot_SurvivesClientDisconnect(t *testing.T) {
	for _, acquired := range []bool{false, true} {
		t.Run(map[bool]string{false: "wait plan", true: "scheduler acquired"}[acquired], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/cachedContents", nil).WithContext(ctx)
			cache := &profitCountingConcurrencyCache{}
			helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
			var schedulerReleases atomic.Int64
			selection := &service.AccountSelectionResult{
				Account: &service.Account{ID: 900}, Acquired: acquired,
				ReleaseFunc: func() { schedulerReleases.Add(1) },
				WaitPlan:    &service.AccountWaitPlan{MaxConcurrency: 1, MaxWaiting: 1, Timeout: time.Second},
			}
			release, ok := (&GatewayHandler{}).acquireGeminiCachedContentAccountSlot(c, zap.NewNop(), helper, selection)
			require.True(t, ok)
			defer release()
			cancel()
			require.Never(t, func() bool { return schedulerReleases.Load()+cache.accountReleases.Load() != 0 },
				50*time.Millisecond, time.Millisecond, "detached cache creation still owns the account slot")
			release()
			release()
			require.Equal(t, int64(1), schedulerReleases.Load()+cache.accountReleases.Load())
		})
	}
}

type cachedContentReviewConcurrencyCache struct {
	fakeConcurrencyCache
	userReleases atomic.Int64
}

func (c *cachedContentReviewConcurrencyCache) ReleaseUserSlot(context.Context, int64, string) error {
	c.userReleases.Add(1)
	return nil
}

type cachedContentReviewUpstream struct {
	*cachedBillingUpstream
	onCreate func()
}

func (u *cachedContentReviewUpstream) Do(req *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	if req.Method == http.MethodPost {
		u.onCreate()
	}
	return u.cachedBillingUpstream.Do(req, proxy, accountID, concurrency)
}

func TestGeminiCachedContentsCreate_UserSlotSurvivesClientDisconnect(t *testing.T) {
	env := newCachedBillingEnv(t, 500, 900)
	env.upstream.createBody = cachedBillingCreateBody(time.Now().Add(time.Hour))
	cache := &cachedContentReviewConcurrencyCache{}
	env.h.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	upstream := &cachedContentReviewUpstream{cachedBillingUpstream: env.upstream, onCreate: func() {
		cancel()
		require.Never(t, func() bool { return cache.userReleases.Load() != 0 }, 50*time.Millisecond, time.Millisecond,
			"detached cache creation still owns the user slot")
	}}
	compat := service.NewGeminiMessagesCompatService(&cachedBillingAccountRepo{accounts: env.accounts}, nil, nil, nil, nil, nil, upstream, nil, &config.Config{})
	env.h.geminiCachedContentService = service.NewGeminiCachedContentService(env.repo, compat, &config.Config{})
	rec := env.serve(http.MethodPost, "/v1beta/cachedContents", `{"model":"gemini-2.5-pro"}`, nil, func(c *gin.Context) {
		c.Request = c.Request.WithContext(ctx)
		env.h.GeminiCachedContentsCreate(c)
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, int64(1), cache.userReleases.Load())
	require.Len(t, env.waitForUsage(t, 1), 1)
}

type cachedContentReviewSaveFailure struct{ *cachedBillingRepo }

func (*cachedContentReviewSaveFailure) Create(context.Context, *service.GeminiCachedContent) error {
	return errors.New("cached content insert failed")
}

func TestGeminiCachedContentsCreate_SaveFailureBillsInputOnly(t *testing.T) {
	env := newCachedBillingEnv(t, 500, 900)
	env.upstream.createBody = cachedBillingCreateBody(time.Now().Add(24 * time.Hour))
	compat := service.NewGeminiMessagesCompatService(&cachedBillingAccountRepo{accounts: env.accounts}, nil, nil, nil, nil, nil, env.upstream, nil, &config.Config{})
	env.h.geminiCachedContentService = service.NewGeminiCachedContentService(&cachedContentReviewSaveFailure{env.repo}, compat, &config.Config{})
	rec := env.create()
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	require.Equal(t, 1, env.upstream.methodCalls(http.MethodDelete))
	require.Empty(t, env.repo.records)
	totals := env.waitForUsage(t, 1)
	require.Len(t, totals, 1)
	require.InDelta(t, cachedBillingTokens*cachedBillingInputPrice, totals[0], 1e-9)
}

func TestGeminiCachedContentsPatch_DelayDoesNotIncreaseCharge(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ttl     string
		balance float64
		charge  bool
	}{
		{name: "shortening without a reservation", ttl: "3599s", balance: 0.0001},
		{name: "extension near balance limit", ttl: "3601s", balance: 0.0025, charge: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newCachedBillingEnv(t, tc.balance, 900)
			cache := &cachedBillingReservationCache{balance: tc.balance}
			enableCachedBillingReservations(t, env, cache)
			baseline := time.Now().Add(time.Hour)
			env.seed(baseline)
			var reserved float64
			env.upstream.beforePost = func(int) {
				cache.mu.Lock()
				for _, amount := range cache.reserved["user:3"] {
					reserved += amount
				}
				cache.mu.Unlock()
				// The upstream applies a relative TTL after this latency. An absolute
				// expiration must retain the amount approved before the call.
				time.Sleep(1500 * time.Millisecond)
			}
			rec := env.patchTTL("abc", tc.ttl)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			if !tc.charge {
				require.Zero(t, reserved)
				require.Empty(t, env.usage.totals(), "shortening must not become a paid extension during the upstream call")
				require.True(t, env.repo.records["abc"].ExpireTime.Before(baseline))
				return
			}
			require.Positive(t, reserved)
			totals := env.waitForUsage(t, 1)
			require.Len(t, totals, 1)
			require.LessOrEqual(t, totals[0], reserved+1e-9, "settlement must not exceed the preflight reservation")
			require.LessOrEqual(t, totals[0], tc.balance)
		})
	}
}

func TestMainAuditCachedPatchPassesQuoteThroughUpstreamWork(t *testing.T) {
	env := newCachedBillingEnv(t, 500, 900)
	cache := &cachedBillingReservationCache{balance: 500}
	enableCachedBillingReservations(t, env, cache)
	env.seed(time.Now().Add(time.Hour))
	var reserved float64
	env.upstream.beforePost = func(int) {
		cache.mu.Lock()
		for _, amount := range cache.reserved["user:3"] {
			reserved += amount
		}
		cache.mu.Unlock()
		env.group.RateMultiplier = 10
	}
	rec := env.patchTTL("abc", "7200s")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	env.waitForUsage(t, 1)
	require.Positive(t, reserved)
	require.InDelta(t, reserved, env.usage.first().ActualCost, 1e-9)
	require.Equal(t, 1.0, env.usage.first().RateMultiplier)
}
