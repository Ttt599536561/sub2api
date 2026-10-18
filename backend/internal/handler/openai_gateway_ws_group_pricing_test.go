package handler

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 分组调价对已打开的 WebSocket 连接生效：同一条连接的两个 turn 之间由管理端
// 改分组倍率（并按分组失效认证缓存），第二个 turn 必须按新倍率计费、按新售价
// 过利润门；HTTP 请求每次经认证缓存取快照，本来就是这个行为。

const (
	wsGroupPricingGroupID = int64(4201) // 与 runOpenAIResponsesWebSocketUsageLogCase 的调度分组一致
	wsGroupPricingKey     = "sk-ws-group-pricing"
	wsGroupPricingFirst   = `{"type":"response.create","model":"gpt-5.6-sol","stream":false}`
	wsGroupPricingSecond  = `{"type":"response.create","model":"gpt-5.6-sol","stream":false}`
)

// wsGroupPricingAPIKeyRepoStub 只实现认证查询与按分组列 Key（认证缓存失效用）。
type wsGroupPricingAPIKeyRepoStub struct {
	service.APIKeyRepository
	mu      sync.Mutex
	apiKey  service.APIKey
	group   service.Group
	err     error
	lookups int
}

func newWSGroupPricingAPIKeyRepoStub(rate float64) *wsGroupPricingAPIKeyRepoStub {
	groupID := wsGroupPricingGroupID
	return &wsGroupPricingAPIKeyRepoStub{
		apiKey: service.APIKey{
			ID:      1801,
			UserID:  1701,
			Key:     wsGroupPricingKey,
			Status:  service.StatusActive,
			GroupID: &groupID,
			User:    &service.User{ID: 1701, Status: service.StatusActive, Concurrency: 1},
		},
		group: service.Group{
			ID:             groupID,
			Name:           "ws-group-pricing",
			Platform:       service.PlatformOpenAI,
			Status:         service.StatusActive,
			RateMultiplier: rate,
			Hydrated:       true,
		},
	}
}

func (s *wsGroupPricingAPIKeyRepoStub) GetByKeyForAuth(ctx context.Context, key string) (*service.APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lookups++
	if s.err != nil {
		return nil, s.err
	}
	if key != s.apiKey.Key {
		return nil, service.ErrAPIKeyNotFound
	}
	apiKey := s.apiKey
	user := *s.apiKey.User
	apiKey.User = &user
	group := s.group
	apiKey.Group = &group
	return &apiKey, nil
}

func (s *wsGroupPricingAPIKeyRepoStub) ListKeysByGroupID(ctx context.Context, groupID int64) ([]string, error) {
	return []string{s.apiKey.Key}, nil
}

// adminUpdate 模拟管理端改 Key/分组后按分组失效认证缓存（与 admin 分组更新同一入口）。
func (s *wsGroupPricingAPIKeyRepoStub) adminUpdate(apiKeyService *service.APIKeyService, fn func(apiKey *service.APIKey, group *service.Group)) error {
	s.mu.Lock()
	fn(&s.apiKey, &s.group)
	s.mu.Unlock()
	apiKeyService.InvalidateAuthCacheByGroupID(context.Background(), wsGroupPricingGroupID)
	return nil
}

func (s *wsGroupPricingAPIKeyRepoStub) lookupCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lookups
}

func newWSGroupPricingAPIKeyService(repo *wsGroupPricingAPIKeyRepoStub) *service.APIKeyService {
	return service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, &config.Config{})
}

func TestOpenAIResponsesWebSocket_GroupRateChangeAppliesToNextTurn(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			repo := newWSGroupPricingAPIKeyRepoStub(3.0)
			apiKeyService := newWSGroupPricingAPIKeyService(repo)
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload:     wsGroupPricingFirst,
				secondPayload:    wsGroupPricingSecond,
				ingressMode:      mode,
				apiKeyService:    apiKeyService,
				apiKeyCredential: wsGroupPricingKey,
				afterFirstUpstreamRequest: func(*service.ChannelService) error {
					return repo.adminUpdate(apiKeyService, func(_ *service.APIKey, group *service.Group) { group.RateMultiplier = 0.3 })
				},
			})

			require.Len(t, got.logs, 2)
			require.InDelta(t, 3.0, got.logs[0].RateMultiplier, 1e-12, "turn 1 bills at the rate in force at connect")
			require.InDelta(t, 0.3, got.logs[1].RateMultiplier, 1e-12, "turn 2 must bill at the group rate in force when it starts")
			require.Greater(t, got.logs[0].ActualCost, 0.0)
			require.InDelta(t, got.logs[0].ActualCost/10, got.logs[1].ActualCost, 1e-15)
		})
	}
}

func TestOpenAIResponsesWebSocket_GroupRateChangeReachesProfitGateOnNextTurn(t *testing.T) {
	repo := newWSGroupPricingAPIKeyRepoStub(3.0)
	repo.group.ProfitControlEnabled = true
	repo.group.ProfitMinMargin = 0.1
	apiKeyService := newWSGroupPricingAPIKeyService(repo)
	accountRate := 1.0
	runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload:          wsGroupPricingFirst,
		secondPayload:         wsGroupPricingSecond,
		apiKeyService:         apiKeyService,
		apiKeyCredential:      wsGroupPricingKey,
		accountRateMultiplier: &accountRate,
		// 3.0 → 0.3：账号倍率 1.0 在新售价下已越线，第二个 turn 必须在转发上游前被拒。
		afterFirstUpstreamRequest: func(*service.ChannelService) error {
			return repo.adminUpdate(apiKeyService, func(_ *service.APIKey, group *service.Group) { group.RateMultiplier = 0.3 })
		},
		secondTurnCloseExpected: true,
		closeStatus:             coderws.StatusTryAgainLater,
		closeReason:             "no longer eligible for this connection",
	})
}

func TestOpenAIResponsesWebSocket_KeyMovedToAnotherGroupKeepsConnectionGroup(t *testing.T) {
	repo := newWSGroupPricingAPIKeyRepoStub(3.0)
	apiKeyService := newWSGroupPricingAPIKeyService(repo)
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload:     wsGroupPricingFirst,
		secondPayload:    wsGroupPricingSecond,
		apiKeyService:    apiKeyService,
		apiKeyCredential: wsGroupPricingKey,
		afterFirstUpstreamRequest: func(*service.ChannelService) error {
			return repo.adminUpdate(apiKeyService, func(apiKey *service.APIKey, group *service.Group) {
				other := wsGroupPricingGroupID + 1
				apiKey.GroupID = &other
				group.ID = other
				group.RateMultiplier = 0.3
			})
		},
	})

	require.Len(t, got.logs, 2)
	require.InDelta(t, 3.0, got.logs[1].RateMultiplier, 1e-12,
		"a key moved to another group keeps the connection's group: the connection was scheduled from it")
}

func TestOpenAIResponsesWebSocket_FailedKeyRefreshKeepsConnectionGroup(t *testing.T) {
	repo := newWSGroupPricingAPIKeyRepoStub(3.0)
	apiKeyService := newWSGroupPricingAPIKeyService(repo)
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload:     wsGroupPricingFirst,
		secondPayload:    wsGroupPricingSecond,
		apiKeyService:    apiKeyService,
		apiKeyCredential: wsGroupPricingKey,
		afterFirstUpstreamRequest: func(*service.ChannelService) error {
			return repo.adminUpdate(apiKeyService, func(_ *service.APIKey, group *service.Group) {
				group.RateMultiplier = 0.3
				repo.err = errors.New("auth store unavailable")
			})
		},
	})

	require.Len(t, got.logs, 2)
	require.InDelta(t, 3.0, got.logs[1].RateMultiplier, 1e-12, "a failed refresh keeps the connection snapshot")
	require.GreaterOrEqual(t, repo.lookupCount(), 2, "the second turn must have attempted a refresh")
}

type wsTurnAPIKeyLookupFunc func(ctx context.Context, key string) (*service.APIKey, error)

func (f wsTurnAPIKeyLookupFunc) GetByKey(ctx context.Context, key string) (*service.APIKey, error) {
	return f(ctx, key)
}

func TestRefreshOpenAIWSTurnBillingAPIKey(t *testing.T) {
	groupID := int64(77)
	conn := &service.APIKey{
		ID:      5,
		Key:     "sk-conn",
		GroupID: &groupID,
		User:    &service.User{ID: 9},
		Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 3},
	}
	latestWith := func(mutate func(k *service.APIKey)) *service.APIKey {
		gid := groupID
		k := &service.APIKey{
			ID:      5,
			Key:     "sk-conn",
			GroupID: &gid,
			User:    &service.User{ID: 9, Balance: 123},
			Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 0.3},
		}
		if mutate != nil {
			mutate(k)
		}
		return k
	}

	t.Run("same group adopts the latest group and nothing else", func(t *testing.T) {
		got := refreshOpenAIWSTurnBillingAPIKey(context.Background(), wsTurnAPIKeyLookupFunc(func(ctx context.Context, key string) (*service.APIKey, error) {
			require.Equal(t, "sk-conn", key)
			return latestWith(nil), nil
		}), conn)
		require.NotSame(t, conn, got)
		require.InDelta(t, 0.3, got.Group.RateMultiplier, 1e-12)
		require.Same(t, conn.User, got.User, "only the group snapshot is refreshed")
		require.Equal(t, conn.ID, got.ID)
		require.InDelta(t, 3.0, conn.Group.RateMultiplier, 1e-12, "the connection snapshot is never mutated")
	})

	keep := map[string]wsTurnAPIKeyLookupFunc{
		"lookup error": func(ctx context.Context, key string) (*service.APIKey, error) {
			return nil, errors.New("boom")
		},
		"not found": func(ctx context.Context, key string) (*service.APIKey, error) {
			return nil, service.ErrAPIKeyNotFound
		},
		"nil key": func(ctx context.Context, key string) (*service.APIKey, error) { return nil, nil },
		"different key id": func(ctx context.Context, key string) (*service.APIKey, error) {
			return latestWith(func(k *service.APIKey) { k.ID = 6 }), nil
		},
		"moved group": func(ctx context.Context, key string) (*service.APIKey, error) {
			return latestWith(func(k *service.APIKey) { other := int64(78); k.GroupID = &other; k.Group.ID = other }), nil
		},
		"no group": func(ctx context.Context, key string) (*service.APIKey, error) {
			return latestWith(func(k *service.APIKey) { k.Group = nil }), nil
		},
		"platform changed": func(ctx context.Context, key string) (*service.APIKey, error) {
			return latestWith(func(k *service.APIKey) { k.Group.Platform = service.PlatformAnthropic }), nil
		},
		"billing type changed": func(ctx context.Context, key string) (*service.APIKey, error) {
			return latestWith(func(k *service.APIKey) { k.Group.SubscriptionType = service.SubscriptionTypeSubscription }), nil
		},
	}
	for name, lookup := range keep {
		t.Run(name+" keeps the connection snapshot", func(t *testing.T) {
			require.Same(t, conn, refreshOpenAIWSTurnBillingAPIKey(context.Background(), lookup, conn))
		})
	}

	t.Run("no credential or no group skips the lookup", func(t *testing.T) {
		called := false
		lookup := wsTurnAPIKeyLookupFunc(func(ctx context.Context, key string) (*service.APIKey, error) {
			called = true
			return latestWith(nil), nil
		})
		noKey := *conn
		noKey.Key = ""
		require.Same(t, &noKey, refreshOpenAIWSTurnBillingAPIKey(context.Background(), lookup, &noKey))
		noGroup := *conn
		noGroup.Group = nil
		require.Same(t, &noGroup, refreshOpenAIWSTurnBillingAPIKey(context.Background(), lookup, &noGroup))
		require.Nil(t, refreshOpenAIWSTurnBillingAPIKey(context.Background(), lookup, nil))
		require.Same(t, conn, refreshOpenAIWSTurnBillingAPIKey(context.Background(), nil, conn))
		require.False(t, called)
	})
}

func TestOpenAIWSTurnBillingAPIKeysKeepPreviousTurnUntilItIsRecorded(t *testing.T) {
	conn := &service.APIKey{ID: 1}
	turn2 := &service.APIKey{ID: 2}
	turn3 := &service.APIKey{ID: 3}
	var keys openAIWSTurnBillingAPIKeys
	require.Same(t, conn, keys.forTurn(1, conn), "a turn without BeforeTurn bills with the connection snapshot")
	keys.set(2, turn2)
	keys.set(3, turn3) // turn 3 starts before turn 2's usage is submitted
	require.Same(t, turn2, keys.forTurn(2, conn))
	require.Same(t, turn3, keys.forTurn(3, conn))
	keys.set(4, conn)
	require.Same(t, conn, keys.forTurn(2, conn), "only the current and previous turn are kept")
	require.Len(t, keys.keys, 2)
}

// The forwarding policy is fixed at connection time, while prices are refreshed
// for each turn. A policy toggle must not retroactively add or remove the 8x
// billing qualification on an already-open connection.
func TestOpenAIResponsesWebSocket_UltrafastPolicySnapshotSurvivesGroupRefresh(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			for _, tc := range []struct {
				name               string
				model              string
				initialUltrafast   bool
				clientTier         string
				wantConnectionTier string
				wantReconnectTier  string
				reconnectFactor    float64
			}{
				{
					name: "disable_after_connect", model: "gpt-6-astra", initialUltrafast: true,
					wantConnectionTier: "ultrafast", reconnectFactor: 1,
				},
				{
					name: "enable_after_connect_with_explicit_tier", model: "gpt-6.1-sol", clientTier: "ultrafast",
					wantConnectionTier: "ultrafast", wantReconnectTier: "ultrafast", reconnectFactor: 8,
				},
				{
					name: "non_target_model", model: "gpt-5.6-sol", initialUltrafast: true,
					reconnectFactor: 1,
				},
			} {
				t.Run(tc.name, func(t *testing.T) {
					repo := newWSGroupPricingAPIKeyRepoStub(3)
					repo.group.ForceOpenAIUltrafast = tc.initialUltrafast

					keyService := newWSGroupPricingAPIKeyService(repo)
					payload := `{"type":"response.create","model":"` + tc.model + `","stream":false}`
					if tc.clientTier != "" {
						payload = `{"type":"response.create","model":"` + tc.model + `","stream":false,"service_tier":"` + tc.clientTier + `"}`
					}
					got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
						firstPayload: payload, secondPayload: payload, ingressMode: mode,
						apiKeyService: keyService, apiKeyCredential: wsGroupPricingKey,
						afterFirstUpstreamRequest: func(*service.ChannelService) error {
							return repo.adminUpdate(keyService, func(_ *service.APIKey, group *service.Group) {
								group.ForceOpenAIUltrafast = !tc.initialUltrafast
								group.RateMultiplier = 0.3
							})
						},
					})
					require.Len(t, got.logs, 2)
					require.Len(t, got.upstreamPayloads, 2)
					for turn, body := range got.upstreamPayloads {
						require.Equal(t, tc.wantConnectionTier, gjson.GetBytes(body, "service_tier").String(), "turn %d uses the connection policy", turn+1)
						require.Equal(t, tc.model, gjson.GetBytes(body, "model").String())
					}
					// Both turns use the same model, tier and token counts. Only the
					// group rate may change their charge on an established connection.
					require.Greater(t, got.logs[0].TotalCost, 0.0)
					require.InDelta(t, 3, got.logs[0].RateMultiplier, 1e-12)
					require.InDelta(t, got.logs[0].TotalCost*3, got.logs[0].ActualCost, 1e-12)
					require.InDelta(t, got.logs[0].TotalCost, got.logs[1].TotalCost, 1e-12,
						"turn 2 keeps the connection's Ultrafast billing qualification")
					require.InDelta(t, 0.3, got.logs[1].RateMultiplier, 1e-12)
					require.InDelta(t, got.logs[0].ActualCost/10, got.logs[1].ActualCost, 1e-12,
						"turn 2 applies the new group rate to the preserved policy")

					// Reauthenticate through the same service after cache invalidation.
					// The new connection omits a tier so its policy must come from the updated group.
					reconnected := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
						firstPayload: `{"type":"response.create","model":"` + tc.model + `","stream":false}`,
						ingressMode:  mode, apiKeyService: keyService, apiKeyCredential: wsGroupPricingKey,
					})
					require.Equal(t, tc.wantReconnectTier, gjson.GetBytes(reconnected.upstreamFirstPayload, "service_tier").String())
					standard, err := service.NewBillingService(&config.Config{}, nil).CalculateCost(
						tc.model, service.UsageTokens{InputTokens: 2, OutputTokens: 1}, 1,
					)
					require.NoError(t, err)
					wantReconnectTotal := standard.TotalCost * tc.reconnectFactor
					require.InDelta(t, wantReconnectTotal, reconnected.log.TotalCost, 1e-12)
					require.InDelta(t, wantReconnectTotal*0.3, reconnected.log.ActualCost, 1e-12,
						"a new connection adopts the current policy and group rate")
				})
			}
		})
	}
}

func TestRefreshOpenAIWSTurnBillingAPIKey_PreservesUltrafastWithoutMutatingSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name             string
		initialUltrafast bool
	}{
		{name: "disable_after_connect", initialUltrafast: true},
		{name: "enable_after_connect", initialUltrafast: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groupID := int64(77)
			conn := &service.APIKey{
				ID: 5, Key: "sk-conn", GroupID: &groupID, User: &service.User{ID: 9},
				Group: &service.Group{
					ID: groupID, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard,
					ForceOpenAIUltrafast: tc.initialUltrafast, RateMultiplier: 3,
				},
			}
			inputPrice, outputPrice, searchPrice := 0.001, 0.002, 0.003
			latest := &service.APIKey{
				ID: 5, Key: "sk-conn", GroupID: &groupID, User: &service.User{ID: 9, Balance: 123},
				Group: &service.Group{
					ID: groupID, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard,
					ForceOpenAIUltrafast: !tc.initialUltrafast, ForceOpenAIFast: tc.initialUltrafast,
					FreeOpenAIFast: true, RateMultiplier: 0.3,
					ProfitControlEnabled: true, ProfitMinMargin: 0.1, ProfitSafetyBuffer: 0.05,
					WebSearchPricePerCall: &searchPrice, LongContextPricingEnabled: true,
					ModelPricing: []service.ChannelModelPricing{{
						Models: []string{"gpt-6-astra"}, BillingMode: service.BillingModeToken,
						InputPrice: &inputPrice, OutputPrice: &outputPrice,
					}},
				},
			}
			connBefore, latestBefore := *conn.Group, *latest.Group
			got := refreshOpenAIWSTurnBillingAPIKey(context.Background(), wsTurnAPIKeyLookupFunc(func(context.Context, string) (*service.APIKey, error) {
				return latest, nil
			}), conn)

			require.Equal(t, connBefore, *conn.Group, "refresh must not mutate the connection group")
			require.Equal(t, latestBefore, *latest.Group, "refresh must not mutate the auth-cache group used by new connections")
			require.NotSame(t, conn, got)
			require.NotSame(t, conn.Group, got.Group)
			require.Equal(t, tc.initialUltrafast, got.Group.ForceOpenAIUltrafast, "billing qualification must match the forwarding policy fixed at connect")
			require.NotSame(t, latest.Group, got.Group, "the per-turn policy must not alias the shared auth-cache group")
			require.Same(t, conn.User, got.User, "refresh changes only group billing data")
			wantGroup := latestBefore
			wantGroup.ForceOpenAIUltrafast = tc.initialUltrafast
			require.Equal(t, wantGroup, *got.Group,
				"only Ultrafast qualification stays fixed; prices, rate, Free Fast and profit settings still refresh")
		})
	}
}

func TestOpenAIResponsesWebSocket_CyberTurnUsesRefreshedUltrafastBillingSnapshot(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			repo := newWSGroupPricingAPIKeyRepoStub(3)
			repo.group.ForceOpenAIUltrafast = true
			keyService := newWSGroupPricingAPIKeyService(repo)
			const payload = `{"type":"response.create","model":"gpt-6-astra","stream":false}`
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload: payload, secondPayload: payload, ingressMode: mode, cyberTurn: 2,
				apiKeyService: keyService, apiKeyCredential: wsGroupPricingKey,
				afterFirstUpstreamRequest: func(*service.ChannelService) error {
					return repo.adminUpdate(keyService, func(_ *service.APIKey, group *service.Group) {
						group.ForceOpenAIUltrafast = false
						group.RateMultiplier = 0.3
					})
				},
			})

			require.Len(t, got.logs, 2, "one normal turn and one failed turn each produce exactly one usage log")
			require.Len(t, got.upstreamPayloads, 2)
			for _, body := range got.upstreamPayloads {
				require.Equal(t, "ultrafast", gjson.GetBytes(body, "service_tier").String())
			}
			require.Equal(t, service.RequestTypeCyberBlocked, got.logs[1].RequestType)
			require.Equal(t, 2, got.logs[1].InputTokens)
			require.Equal(t, 1, got.logs[1].OutputTokens)
			require.NotNil(t, got.logs[1].ServiceTier)
			require.Equal(t, "ultrafast", *got.logs[1].ServiceTier, "a failed response without a tier retains the sent Ultrafast tier")
			require.InDelta(t, 0.3, got.logs[1].RateMultiplier, 1e-12,
				"cyber error settlement must use the current turn's refreshed group rate")
			standard, err := service.NewBillingService(&config.Config{}, nil).CalculateCost(
				"gpt-6-astra", service.UsageTokens{InputTokens: 2, OutputTokens: 1}, 1,
			)
			require.NoError(t, err)
			require.InDelta(t, standard.TotalCost*8, got.logs[1].TotalCost, 1e-12,
				"the failed turn keeps the established connection's Ultrafast billing qualification")
			require.InDelta(t, standard.TotalCost*8*0.3, got.logs[1].ActualCost, 1e-12)
			require.InDelta(t, got.logs[0].ActualCost/10, got.logs[1].ActualCost, 1e-12)
		})
	}
}
