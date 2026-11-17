package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type wsTurnReservationRouteKey struct{}

type wsTurnReservationGateway struct {
	estimates []service.InflightEstimateRequest
	routes    []string
}

func (g *wsTurnReservationGateway) WithOpenAIWSModelRouting(ctx context.Context, payload []byte) context.Context {
	return context.WithValue(ctx, wsTurnReservationRouteKey{}, string(payload))
}

func (g *wsTurnReservationGateway) EstimateInflightReservation(ctx context.Context, _ *service.APIKey, req service.InflightEstimateRequest) (float64, bool) {
	g.estimates = append(g.estimates, req)
	route, _ := ctx.Value(wsTurnReservationRouteKey{}).(string)
	g.routes = append(g.routes, route)
	return float64(req.MaxTokens), true
}

type wsTurnSupplementRepo struct {
	service.UserSubscriptionRepository
	sub         *service.UserSubscription
	mu          sync.Mutex
	holds       map[string]float64
	transferErr error
}

func (r *wsTurnSupplementRepo) GetByID(context.Context, int64) (*service.UserSubscription, error) {
	copy := *r.sub
	return &copy, nil
}

func (r *wsTurnSupplementRepo) ReserveSubscriptionSupplement(_ context.Context, hold *service.SubscriptionSupplementReservation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var pending float64
	for _, amount := range r.holds {
		pending += amount
	}
	if pending+hold.AmountUSD > r.sub.AvailableQuota(r.sub.Group) {
		return service.ErrDailyLimitExceeded
	}
	if r.holds == nil {
		r.holds = make(map[string]float64)
	}
	r.holds[hold.ID] = hold.AmountUSD
	return nil
}
func (r *wsTurnSupplementRepo) RenewSubscriptionSupplementReservation(_ context.Context, id string, _ time.Duration) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.holds[id]
	return ok, nil
}
func (r *wsTurnSupplementRepo) ReleaseSubscriptionSupplementReservation(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.holds, id)
	return nil
}
func (r *wsTurnSupplementRepo) TransferSubscriptionSupplementReservation(context.Context, string, string, time.Duration) error {
	return r.transferErr
}

type wsTurnSupplementCache struct {
	service.BillingCache
	mu      sync.Mutex
	pending map[string]float64
	durable *wsTurnSupplementRepo
}

func (c *wsTurnSupplementCache) ReserveInflightScope(_ context.Context, scope, id string, amount float64, _ time.Duration) (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	total := 0.0
	for _, amount := range c.pending {
		total += amount
	}
	c.pending[scope+"/"+id] = amount
	return total, nil
}

func (c *wsTurnSupplementCache) ReleaseInflightScope(_ context.Context, scope, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, scope+"/"+id)
	return nil
}

func (c *wsTurnSupplementCache) RenewInflightScope(_ context.Context, scope, id string, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.pending[scope+"/"+id]
	return ok, nil
}

func (c *wsTurnSupplementCache) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending)
}

func wsTurnSupplementFixture(t *testing.T) (*service.BillingCacheService, *wsTurnSupplementCache, *service.APIKey, *service.UserSubscription) {
	t.Helper()
	limit := 1.0
	group := &service.Group{ID: 8, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &limit}
	sub := &service.UserSubscription{ID: 13, UserID: 5, GroupID: 8, Group: group, Status: service.StatusActive,
		StartsAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour), DailyUsageUSD: 1, SupplementGrantedUSD: 10}
	repo := &wsTurnSupplementRepo{sub: sub}
	cache := &wsTurnSupplementCache{pending: make(map[string]float64), durable: repo}
	billing := service.NewBillingCacheService(cache, nil, repo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(billing.Stop)
	return billing, cache, &service.APIKey{ID: 7, User: &service.User{ID: 5}, GroupID: &group.ID, Group: group}, sub
}

func TestOpenAIWSTurnReservation_EstimatesEachPayloadAndKeepsAsyncUsage(t *testing.T) {
	billing, cache, key, sub := wsTurnSupplementFixture(t)
	var turns openAIWSTurnReservations
	defer turns.releaseAll()
	gateway := &wsTurnReservationGateway{}
	first := []byte(`{"type":"response.create","model":"first-model","max_output_tokens":2}`)
	second := []byte(`{"type":"response.create","model":"second-model","max_output_tokens":3,"input":"longer input"}`)
	require.NoError(t, turns.reserve(context.Background(), 1, billing, gateway, key, sub, "first-model", first))
	firstCtx, firstDone := turns.take(1, context.Background())
	require.NotNil(t, service.InflightReservationFromContext(firstCtx))
	require.Equal(t, 2.0, service.InflightReservationFromContext(firstCtx).Amount())
	firstTask, abandonFirst := wrapUsageRecordTaskContext(firstCtx, func(context.Context) {})
	defer abandonFirst()
	firstDone()
	require.Equal(t, 1, cache.count(), "the first usage task must hold its reservation after AfterTurn")

	require.NoError(t, turns.reserve(context.Background(), 2, billing, gateway, key, sub, "second-model", second))
	secondCtx, secondDone := turns.take(2, context.Background())
	require.NotSame(t, service.InflightReservationFromContext(firstCtx), service.InflightReservationFromContext(secondCtx))
	require.Equal(t, 3.0, service.InflightReservationFromContext(secondCtx).Amount())
	secondTask, abandonSecond := wrapUsageRecordTaskContext(secondCtx, func(context.Context) {})
	defer abandonSecond()
	secondDone()
	turns.releaseAll()
	require.Equal(t, 2, cache.count(), "connection cleanup cannot release either queued usage task")
	require.Equal(t, []string{string(first), string(second)}, gateway.routes)
	require.Equal(t, "second-model", gateway.estimates[1].Model)
	require.Equal(t, len(second), gateway.estimates[1].BodyBytes)
	require.Equal(t, 3, gateway.estimates[1].MaxTokens)
	firstTask(context.Background())
	require.Equal(t, 1, cache.count(), "the old turn must not release the new turn")
	secondTask(context.Background())
	require.Zero(t, cache.count())
}

func TestOpenAIWSTurnReservation_ReleasesErrorCloseAndFailover(t *testing.T) {
	billing, cache, key, sub := wsTurnSupplementFixture(t)
	var turns openAIWSTurnReservations
	gateway := &wsTurnReservationGateway{}
	payload := []byte(`{"max_output_tokens":2}`)
	for _, cause := range []string{"no usage", "BeforeTurn failure", "client close", "failover"} {
		t.Run(cause, func(t *testing.T) {
			require.NoError(t, turns.reserve(context.Background(), 1, billing, gateway, key, sub, "model", payload))
			require.Equal(t, 1, cache.count())
			if cause == "no usage" {
				_, done := turns.take(1, context.Background())
				done()
				done()
			} else {
				turns.releaseAll()
				turns.releaseAll()
			}
			require.Zero(t, cache.count())
		})
	}
	// A retry starts a fresh turn 1 with its own payload and no leaked predecessor.
	require.NoError(t, turns.reserve(context.Background(), 1, billing, gateway, key, sub, "retry", payload))
	ctx, done := turns.take(1, context.Background())
	_, abandon := wrapUsageRecordTaskContext(ctx, func(context.Context) { t.Fatal("dropped task must not run") })
	done()
	require.Equal(t, 1, cache.count())
	abandon()
	require.Zero(t, cache.count())
}

func TestOpenAIWSTurnReservation_RejectionDoesNotReleaseAnotherTurn(t *testing.T) {
	billing, cache, key, sub := wsTurnSupplementFixture(t)
	var turns openAIWSTurnReservations
	defer turns.releaseAll()
	gateway := &wsTurnReservationGateway{}
	require.NoError(t, turns.reserve(context.Background(), 1, billing, gateway, key, sub, "first", []byte(`{"max_output_tokens":8}`)))
	require.Error(t, turns.reserve(context.Background(), 2, billing, gateway, key, sub, "second", []byte(`{"max_output_tokens":8}`)))
	require.Equal(t, 1, cache.count())
	turns.releaseAll()
	require.Zero(t, cache.count())
}

func TestOpenAIWSTurnReservation_RefreshesCreditAddedAfterConnection(t *testing.T) {
	billing, cache, key, current := wsTurnSupplementFixture(t)
	connectedSnapshot := *current
	connectedSnapshot.SupplementGrantedUSD = 0
	var turns openAIWSTurnReservations
	defer turns.releaseAll()
	require.NoError(t, turns.reserve(context.Background(), 2, billing, &wsTurnReservationGateway{}, key, &connectedSnapshot, "new-model", []byte(`{"max_output_tokens":2}`)))
	require.Equal(t, 1, cache.count(), "a subscription connection must see supplemental credit added after its first frame")
	require.Zero(t, connectedSnapshot.SupplementGrantedUSD, "the original settlement term snapshot must stay immutable")
}

func TestOpenAIWSTurnReservation_DoesNotSwitchToRenewedTerm(t *testing.T) {
	billing, cache, key, current := wsTurnSupplementFixture(t)
	connectedSnapshot := *current
	connectedSnapshot.StartsAt = connectedSnapshot.StartsAt.Add(-24 * time.Hour)
	var turns openAIWSTurnReservations
	defer turns.releaseAll()
	require.ErrorIs(t, turns.reserve(context.Background(), 2, billing, &wsTurnReservationGateway{}, key, &connectedSnapshot, "model", []byte(`{"max_output_tokens":2}`)), service.ErrSubscriptionInvalid)
	require.Zero(t, cache.count(), "a new term must not be billed through a connection to the old term")
}

type wsTurnBlockingUsageRepo struct {
	service.UsageLogRepository
	entered chan struct{}
	resume  chan struct{}
	holdID  string
}

func (r *wsTurnBlockingUsageRepo) Create(ctx context.Context, _ *service.UsageLog) (bool, error) {
	r.holdID = service.SubscriptionSupplementHoldIDFromContext(ctx)
	close(r.entered)
	<-r.resume
	return true, nil
}

func TestOpenAIWSTurnReservation_CyberUsageRetainsFailedTurn(t *testing.T) {
	for _, transport := range []string{"http", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			billing, cache, key, sub := wsTurnSupplementFixture(t)
			var turns openAIWSTurnReservations
			defer turns.releaseAll()
			require.NoError(t, turns.reserve(context.Background(), 1, billing, &wsTurnReservationGateway{}, key, sub, "gpt-5.4", []byte(`{"max_output_tokens":2}`)))
			turnCtx, done := turns.take(1, context.Background())
			defer done()
			repo := &wsTurnBlockingUsageRepo{entered: make(chan struct{}), resume: make(chan struct{})}
			var resumeOnce sync.Once
			resume := func() { resumeOnce.Do(func() { close(repo.resume) }) }
			t.Cleanup(resume)
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Default.RateMultiplier = 1
			gw := service.NewOpenAIGatewayService(nil, repo, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, nil, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
			h := &OpenAIGatewayHandler{gatewayService: gw}
			c := newInflightTestGinContext()
			service.MarkOpsCyberPolicy(c, service.CyberPolicyMark{Message: "blocked", UpstreamStatus: 200, UpstreamInTok: 12, UpstreamOutTok: 3})
			var reservationContexts []context.Context
			if transport == "http" {
				c.Request = c.Request.WithContext(turnCtx)
			} else {
				reservationContexts = []context.Context{turnCtx}
			}
			h.recordCyberPolicyIfMarked(c, key, &service.Account{ID: 17, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}, sub, "gpt-5.4", true, nil, service.ChannelUsageFields{}, "", time.Time{}, reservationContexts...)
			select {
			case <-repo.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("cyber usage writer did not enter the repository")
			}
			require.Equal(t, service.InflightReservationFromContext(turnCtx).SupplementHoldID(), repo.holdID)
			done()
			require.Equal(t, 1, cache.count(), "the failed turn must remain reserved while cyber usage is pending")
			resume()
			require.Eventually(t, func() bool { return cache.count() == 0 }, time.Second, time.Millisecond)
		})
	}
}

func TestDeferredVideoTransferredHoldSurvivesHandlerAndRedisLoss(t *testing.T) {
	for _, transferFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "transfer success", true: "transfer fails after upstream accept"}[transferFails], func(t *testing.T) {
			billing, cache, key, sub := wsTurnSupplementFixture(t)
			var turns openAIWSTurnReservations
			defer turns.releaseAll()
			require.NoError(t, turns.reserve(context.Background(), 1, billing, &wsTurnReservationGateway{}, key, sub, "video", []byte(`{"max_output_tokens":8}`)))
			ctx, done := turns.take(1, context.Background())
			reservation := service.InflightReservationFromContext(ctx)
			if transferFails {
				cache.durable.transferErr = service.ErrBillingServiceUnavailable
			}
			err := reservation.TransferSupplementHold(ctx, "grok-video:task", 24*time.Hour)
			if transferFails {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			done()
			cache.mu.Lock()
			cache.pending = make(map[string]float64)
			cache.mu.Unlock()
			require.Error(t, turns.reserve(context.Background(), 2, billing, &wsTurnReservationGateway{}, key, sub, "video", []byte(`{"max_output_tokens":8}`)), "a second request must respect the durable first hold even with no Redis key")
			require.NoError(t, billing.ReleaseSubscriptionSupplementHold(ctx, reservation.SupplementHoldID()))
			require.NoError(t, turns.reserve(context.Background(), 3, billing, &wsTurnReservationGateway{}, key, sub, "video", []byte(`{"max_output_tokens":8}`)))
		})
	}
}
