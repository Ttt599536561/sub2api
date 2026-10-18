package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGPT55RetirementWSSelectionUsesTransportMapping(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		for _, previous := range []bool{false, true} {
			for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough, OpenAIWSIngressModeHTTPBridge} {
				for _, httpPassthrough := range []bool{false, true} {
					t.Run(fmt.Sprintf("advanced=%t/previous=%t/%s/http_passthrough=%t", advanced, previous, mode, httpPassthrough), func(t *testing.T) {
						defer resetOpenAIAdvancedSchedulerSettingCacheForTest()
						cfg := &config.Config{}
						cfg.Gateway.OpenAIWS.Enabled = true
						cfg.Gateway.OpenAIWS.OAuthEnabled = true
						cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
						cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
						account := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1,
							Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.5": "gpt-6.1-sol"}},
							Extra:       map[string]any{"openai_passthrough": httpPassthrough, "openai_oauth_responses_websockets_v2_enabled": true, "openai_oauth_responses_websockets_v2_mode": mode},
						}
						svc := &OpenAIGatewayService{cfg: cfg, accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
							cache: &schedulerTestGatewayCache{}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
							rateLimitService: newOpenAIAdvancedSchedulerRateLimitService(fmt.Sprint(advanced)),
						}
						ctx := context.Background()
						groupID := int64(42)
						responseID := ""
						if previous {
							responseID = "resp_replacement"
							require.NoError(t, svc.getOpenAIWSStateStore().BindResponseAccount(ctx, groupID, responseID, account.ID, time.Hour))
						}
						selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, &groupID, responseID, "", "gpt-5.5", nil,
							OpenAIUpstreamTransportResponsesWebsocketV2Ingress, "", false, false, true)
						wantAllowed := mode != OpenAIWSIngressModePassthrough
						if wantAllowed {
							require.NoError(t, err)
							require.NotNil(t, selection)
							require.Equal(t, account.ID, selection.Account.ID)
							if selection.ReleaseFunc != nil {
								selection.ReleaseFunc()
							}
						} else {
							require.ErrorIs(t, err, ErrNoAvailableAccounts)
							require.Nil(t, selection)
						}
					})
				}
			}
		}
	}
}

type retirementRecheckAccountRepo struct {
	schedulerTestOpenAIAccountRepo
	reads int
}

func (r *retirementRecheckAccountRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	r.reads++
	return r.schedulerTestOpenAIAccountRepo.GetByID(ctx, id)
}

func TestGPT55RetirementWSSelectionRechecksSnapshotAndPreviousResponse(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		for _, previous := range []bool{false, true} {
			for _, retiredInDB := range []bool{false, true} {
				t.Run(fmt.Sprintf("advanced=%t/previous=%t/retired_in_db=%t", advanced, previous, retiredInDB), func(t *testing.T) {
					defer resetOpenAIAdvancedSchedulerSettingCacheForTest()
					ctx := context.Background()
					groupID := int64(42)
					stale := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID},
						Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.5": "gpt-6.1-sol"}},
						Extra:       map[string]any{"openai_passthrough": true, "openai_oauth_responses_websockets_v2_enabled": true, "openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModeCtxPool},
					}
					fresh := stale
					if retiredInDB {
						fresh.Credentials = map[string]any{"model_mapping": map[string]any{"gpt-5.5": "gpt-5.5"}}
					}
					repo := &retirementRecheckAccountRepo{schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{fresh}}}
					cfg := newSchedulerTestOpenAIWSV2Config()
					cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
					svc := &OpenAIGatewayService{cfg: cfg, accountRepo: repo,
						cache: &schedulerTestGatewayCache{}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
						rateLimitService:  newOpenAIAdvancedSchedulerRateLimitService(fmt.Sprint(advanced)),
						schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{snapshotAccounts: []*Account{&stale}, accountsByID: map[int64]*Account{stale.ID: &stale}}},
					}
					responseID := ""
					if previous {
						responseID = "resp_snapshot_replacement"
						require.NoError(t, svc.getOpenAIWSStateStore().BindResponseAccount(ctx, groupID, responseID, stale.ID, time.Hour))
					}
					selection, decision, err := svc.SelectAccountWithSchedulerForCapability(ctx, &groupID, responseID, "", "gpt-5.5", nil,
						OpenAIUpstreamTransportResponsesWebsocketV2Ingress, "", false, false, true)
					require.Positive(t, repo.reads, "selection must recheck the repository, not just its snapshot")
					if retiredInDB {
						require.ErrorIs(t, err, ErrNoAvailableAccounts)
						require.Nil(t, selection, "a stale live replacement must not conceal the retired database target")
						return
					}
					require.NoError(t, err)
					require.NotNil(t, selection)
					require.Equal(t, stale.ID, selection.Account.ID)
					require.Equal(t, previous, decision.StickyPreviousHit, "continuation must retain its owner instead of silently falling back")
					if selection.ReleaseFunc != nil {
						selection.ReleaseFunc()
					}
				})
			}
		}
	}
}

func TestGPT55RetirementWSDiagnosisExcludesDisabledTransport(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeOff, OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			cfg := newSchedulerTestOpenAIWSV2Config()
			cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
			groupID := int64(42)
			accounts := []Account{
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1,
					Extra: map[string]any{"openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough}},
				{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1,
					Extra: map[string]any{"openai_apikey_responses_websockets_v2_mode": mode}},
			}
			svc := &OpenAIGatewayService{cfg: cfg, accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}}
			wsCtx := svc.WithOpenAIWSModelRouting(context.Background(), []byte(`{"type":"response.create","model":"gpt-5.5"}`))
			diag := svc.DiagnoseModelAvailabilityForPlatform(wsCtx, &groupID, "gpt-5.5", PlatformOpenAI)
			require.True(t, diag.HasAccountsInPool)
			require.True(t, diag.HasRetiredSubscriptionModel)
			require.Equal(t, mode != OpenAIWSIngressModeOff, diag.HasModelSupport,
				"a WS-disabled API key must not turn retired-model policy rejection into a capacity retry")
			httpDiag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), &groupID, "gpt-5.5", PlatformOpenAI)
			require.True(t, httpDiag.HasModelSupport, "HTTP API key exemption is independent of its WS configuration")
		})
	}
}
