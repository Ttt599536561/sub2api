package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWelfareSubscriptionDrawEntitlement(t *testing.T) {
	for _, tc := range []struct {
		name, spend                         string
		subscription, used, available, debt int64
		remaining                           string
	}{
		{"subscription only", "0", 4, 0, 4, 0, "50.00000000"},
		{"API remainder stays independent", "49", 4, 4, 0, 0, "1.00000000"},
		{"both sources", "100", 4, 1, 5, 0, "50.00000000"},
		{"refunded used draws", "0", 2, 4, 0, 2, "150.00000000"},
		{"new purchase pays down debt", "0", 5, 4, 1, 0, "50.00000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			overview, err := welfareOverview(&WelfareState{Wallet: WelfareWallet{EligibleSpend: tc.spend, SubscriptionDraws: tc.subscription, DrawsUsed: tc.used}}, time.Now())
			require.NoError(t, err)
			require.Equal(t, tc.available, overview.AvailableDraws)
			require.Equal(t, tc.debt, overview.TicketDebt)
			require.Equal(t, tc.remaining, overview.NextDrawRemaining)
			require.Equal(t, tc.subscription, overview.SubscriptionDraws)
		})
	}
}

func TestWelfareSubscriptionOnlyUserCanDraw(t *testing.T) {
	launch := time.Now().Add(-time.Hour)
	repo := &welfareDomainRepo{state: WelfareState{Program: WelfareSettings{Enabled: true, LaunchAt: &launch}, AccountBalance: "0", Wallet: WelfareWallet{EligibleSpend: "0", SubscriptionDraws: 1}}}
	svc := NewWelfareService(repo, WithWelfareRandom(welfareSequence(0)))
	result, err := svc.Draw(context.Background(), 1, "subscription-first-draw")
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Overview.DrawsUsed)
	require.Zero(t, result.Overview.AvailableDraws)
	require.Equal(t, "0.50", result.RewardAmount)
	_, err = svc.Draw(context.Background(), 1, "subscription-second-draw")
	require.ErrorIs(t, err, ErrWelfareNoTickets)
	rules, err := svc.Rules(context.Background())
	require.NoError(t, err)
	require.Equal(t, "50.00", rules.SubscriptionDrawThreshold)
	require.Equal(t, "CNY", rules.SubscriptionDrawCurrency)
}
