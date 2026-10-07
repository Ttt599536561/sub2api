//go:build unit

package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type review2WSPersistedBalance struct {
	service.UserRepository
	service.UsageBillingRepository
	mu      sync.Mutex
	balance float64
}

func (r *review2WSPersistedBalance) GetByID(_ context.Context, id int64) (*service.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return &service.User{ID: id, Balance: r.balance, Status: service.StatusActive}, nil
}
func (r *review2WSPersistedBalance) Apply(_ context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.balance -= cmd.BalanceCost
	value := r.balance
	return &service.UsageBillingApplyResult{Applied: true, NewBalance: &value}, nil
}

func TestReview2OpenAIWebSocketRejectsNewTurnAfterSettledBalanceExhaustion(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeDedicated} {
		for _, exhaust := range []bool{false, true} {
			name := mode + "/positive_balance"
			if exhaust {
				name = mode + "/balance_exhausted"
			}
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				var forwarded atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					conn, err := coderws.Accept(w, r, nil)
					if err != nil {
						return
					}
					defer conn.CloseNow()
					for {
						if _, _, err := conn.Read(ctx); err != nil {
							return
						}
						turn := forwarded.Add(1)
						event := fmt.Sprintf(`{"type":"response.completed","response":{"id":"review-turn-%d","model":"gpt-5.4","usage":{"input_tokens":2,"output_tokens":1}}}`, turn)
						if err := conn.Write(ctx, coderws.MessageText, []byte(event)); err != nil {
							return
						}
					}
				}))
				defer upstream.Close()
				account := service.Account{ID: 9901, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
					Status: service.StatusActive, Schedulable: true, Concurrency: 1,
					Credentials: map[string]any{"api_key": "test-key", "base_url": upstream.URL},
					Extra:       map[string]any{"openai_apikey_responses_websockets_v2_enabled": true, "openai_apikey_responses_websockets_v2_mode": mode}}
				cfg := &config.Config{}
				cfg.Default.RateMultiplier = 1
				cfg.Security.URLAllowlist.AllowInsecureHTTP = true
				cfg.Gateway.OpenAIWS.Enabled = true
				cfg.Gateway.OpenAIWS.APIKeyEnabled = true
				cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
				cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
				cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
				cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
				cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
				ledger := &review2WSPersistedBalance{balance: 6}
				if exhaust {
					ledger.balance = 3
				}
				logs := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 2)}
				cache := service.NewBillingCacheService(nil, ledger, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(cache.Stop)
				prices := service.NewBillingService(cfg, nil)
				gateway := service.NewOpenAIGatewayService(&openAIWSUsageHandlerAccountRepoStub{account: account}, logs, ledger, ledger, nil, nil, nil, cfg, nil, nil,
					prices, nil, cache, &compositeWSHTTPUpstream{}, &service.DeferredService{}, nil, nil, service.NewModelPricingResolver(nil, prices), nil, nil, nil, nil)
				slots := &concurrencyCacheMock{
					acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
					acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
				}
				h := &OpenAIGatewayHandler{cfg: cfg, gatewayService: gateway, billingCacheService: cache, apiKeyService: &service.APIKeyService{},
					concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(slots), SSEPingFormatNone, time.Second)}
				groupID, price := int64(4201), 1.0
				key := &service.APIKey{ID: 1801, UserID: 1701, GroupID: &groupID, User: &service.User{ID: 1701, Balance: 6, Status: service.StatusActive},
					Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 1,
						ModelPricing: []service.ChannelModelPricing{{Models: []string{"gpt-5.4"}, BillingMode: service.BillingModeToken, InputPrice: &price, OutputPrice: &price}}}}
				router := gin.New()
				router.GET("/openai/v1/responses", func(c *gin.Context) {
					c.Set(string(middleware2.ContextKeyAPIKey), key)
					c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: key.UserID, Concurrency: 1})
					h.ResponsesWebSocket(c)
				})
				downstream := httptest.NewServer(router)
				defer downstream.Close()
				conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(downstream.URL, "http")+"/openai/v1/responses", nil)
				require.NoError(t, err)
				defer conn.CloseNow()
				payload := []byte(`{"type":"response.create","model":"gpt-5.4","input":"hello","stream":false}`)
				require.NoError(t, conn.Write(ctx, coderws.MessageText, payload))
				_, event, err := conn.Read(ctx)
				require.NoError(t, err)
				require.Contains(t, string(event), "response.completed")
				// A completed event alone may precede async billing. Wait for
				// the actual post-billing log before sending a separate turn.
				select {
				case row := <-logs.created:
					require.Equal(t, 3.0, row.ActualCost)
				case <-ctx.Done():
					t.Fatal("first turn did not settle")
				}
				persisted, err := ledger.GetByID(ctx, key.UserID)
				require.NoError(t, err)
				if exhaust {
					require.Zero(t, persisted.Balance)
				} else {
					require.Equal(t, 3.0, persisted.Balance)
				}
				require.NoError(t, conn.Write(ctx, coderws.MessageText, payload))
				_, next, readErr := conn.Read(ctx)
				if exhaust {
					require.Error(t, readErr, "a new response.create after the settled balance reaches zero must be rejected; got %s", next)
					require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(readErr))
					require.EqualValues(t, 1, forwarded.Load())
				} else {
					require.NoError(t, readErr)
					require.Contains(t, string(next), "response.completed")
					require.EqualValues(t, 2, forwarded.Load())
				}
			})
		}
	}
}

type review2RevalidationRPM struct {
	service.UserRPMCache
	calls int
}

func (r *review2RevalidationRPM) IncrementUserRPM(context.Context, int64) (int, error) {
	r.calls++
	return r.calls, nil
}

type review2RevalidationKey struct {
	service.APIKeyRepository
	used float64
}

func (r *review2RevalidationKey) GetRateLimitData(context.Context, int64) (*service.APIKeyRateLimitData, error) {
	now := time.Now()
	return &service.APIKeyRateLimitData{Usage5h: r.used, Window5hStart: &now, Window1dStart: &now, Window7dStart: &now}, nil
}

type review2RevalidationPlatform struct {
	service.UserPlatformQuotaRepository
	used float64
}

func (r *review2RevalidationPlatform) GetByUserPlatform(_ context.Context, id int64, platform string) (*service.UserPlatformQuotaRecord, error) {
	now, limit := time.Now(), 1.0
	return &service.UserPlatformQuotaRecord{UserID: id, Platform: platform, DailyLimitUSD: &limit, DailyUsageUSD: r.used, DailyWindowStart: &now}, nil
}

type review2RevalidationSubscription struct {
	fresh *service.UserSubscription
	group *service.Group
}

func (r *review2RevalidationSubscription) GetSubscriptionForAdmission(context.Context, int64, int64) (*service.UserSubscription, *service.Group, error) {
	copy := *r.fresh
	return &copy, r.group, nil
}
func (*review2RevalidationSubscription) TriggerAutoDailyReset(context.Context, int64) error {
	return nil
}

func TestReview2RevalidateBillingChecksMoneyWithoutExtraRPM(t *testing.T) {
	ctx := context.Background()
	ledger := &review2WSPersistedBalance{balance: 10}
	rpm, keyRepo, platformRepo := &review2RevalidationRPM{}, &review2RevalidationKey{}, &review2RevalidationPlatform{}
	billing := service.NewBillingCacheService(nil, ledger, nil, keyRepo, rpm, nil, &config.Config{}, platformRepo)
	t.Cleanup(billing.Stop)
	user := &service.User{ID: 1701, RPMLimit: 1}
	key := &service.APIKey{ID: 1801, RateLimit5h: 1}
	group := &service.Group{ID: 4201, Platform: service.PlatformOpenAI, RateMultiplier: 1}
	require.NoError(t, billing.CheckBillingEligibility(ctx, user, key, group, nil, service.PlatformOpenAI))
	for range 2 {
		require.NoError(t, billing.RevalidateBillingEligibility(ctx, user, key, group, nil, service.PlatformOpenAI))
	}
	require.Equal(t, 1, rpm.calls, "financial checks after waiting and on later turns cannot recount RPM")
	keyRepo.used = 1
	require.ErrorIs(t, billing.RevalidateBillingEligibility(ctx, user, key, group, nil, service.PlatformOpenAI), service.ErrAPIKeyRateLimit5hExceeded)
	keyRepo.used = 0
	platformRepo.used = 1
	require.ErrorIs(t, billing.RevalidateBillingEligibility(ctx, user, key, group, nil, service.PlatformOpenAI), service.ErrUserPlatformDailyQuotaExhausted)
	require.Equal(t, 1, rpm.calls)
}

func TestReview2RevalidateBillingDoesNotMutatePreviousTurnSubscription(t *testing.T) {
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(billing.Stop)
	group := &service.Group{ID: 24, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription}
	prior := &service.UserSubscription{ID: 1, UserID: 10, GroupID: group.ID, Status: service.SubscriptionStatusActive,
		DailyUsageUSD: 1, DailyResetVersion: 2, ExpiresAt: time.Now().Add(48 * time.Hour)}
	original := *prior
	fresh := *prior
	fresh.DailyUsageUSD = 7
	fresh.DailyResetVersion = 3
	fresh.ExpiresAt = fresh.ExpiresAt.Add(-24 * time.Hour)
	billing.SetSubscriptionService(&review2RevalidationSubscription{fresh: &fresh, group: group})
	require.NoError(t, billing.RevalidateBillingEligibility(context.Background(), &service.User{ID: 10}, nil, group, prior, service.PlatformOpenAI))
	require.Equal(t, original, *prior, "a prior turn's queued billing must retain its original snapshot")
}

func TestReview2RevalidateBillingPreservesSimpleModeContracts(t *testing.T) {
	keyRepo := &review2RevalidationKey{used: 1}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	billing := service.NewBillingCacheService(nil, nil, nil, keyRepo, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	key := &service.APIKey{ID: 1801, RateLimit5h: 1}
	require.NoError(t, billing.RevalidateBillingEligibility(context.Background(), &service.User{}, key, nil, nil, ""))
	cfg.SimpleModeKeyRateLimitEnabled = true
	require.ErrorIs(t, billing.RevalidateBillingEligibility(context.Background(), &service.User{}, key, nil, nil, ""), service.ErrAPIKeyRateLimit5hExceeded)
}
