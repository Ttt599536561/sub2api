package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGrokVideoPendingKeepsOriginalSubscriptionBinding(t *testing.T) {
	// A job may finish after the same subscription ID has been renewed into a
	// different term. Redis persistence must retain the create-time identity.
	want := `{"user_id":5,"group_id":7,"subscription_id":9,"term_starts_at":"2026-11-01T00:00:00Z","entitlement_kind":"standard"}`
	var pending GrokVideoPendingBilling
	require.NoError(t, json.Unmarshal([]byte(`{"model":"video","subscription_binding":`+want+`}`), &pending))
	encoded, err := json.Marshal(pending)
	require.NoError(t, err)
	var stored map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &stored))
	require.NotEmpty(t, stored["subscription_binding"], "deferred billing must retain the original subscription term")
	require.JSONEq(t, want, string(stored["subscription_binding"]))
}

func TestGrokVideoBillingBindingDoesNotFollowRenewedSubscription(t *testing.T) {
	group := int64(7)
	oldStart := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	original := &UserSubscription{ID: 9, StartsAt: oldStart, EntitlementKind: SubscriptionEntitlementStandard}
	binding := CaptureGrokVideoBillingBinding(5, &group, original)
	current := &UserSubscription{ID: 9, StartsAt: oldStart.AddDate(0, 1, 0), SupplementGrantedUSD: 50}
	restored, err := binding.RestoreSubscription(5, &group, current)
	require.NoError(t, err)
	require.Equal(t, oldStart, restored.StartsAt)
	require.Equal(t, int64(9), restored.ID)
	require.Zero(t, restored.SupplementGrantedUSD, "new term credit must not leak into old job identity")
	require.Equal(t, oldStart.AddDate(0, 1, 0), current.StartsAt)
	_, err = binding.RestoreSubscription(6, &group, current)
	require.ErrorIs(t, err, ErrSubscriptionInvalid)
	otherGroup := int64(8)
	_, err = binding.RestoreSubscription(5, &otherGroup, current)
	require.ErrorIs(t, err, ErrSubscriptionInvalid)
}

func TestGrokVideoBalanceBindingDoesNotAdoptNewSubscription(t *testing.T) {
	group := int64(7)
	current := &UserSubscription{ID: 9}
	binding := CaptureGrokVideoBillingBinding(5, &group, nil)
	restored, err := binding.RestoreSubscription(5, &group, current)
	require.NoError(t, err)
	require.Nil(t, restored)
	var legacy *GrokVideoBillingBinding
	restored, err = legacy.RestoreSubscription(5, &group, current)
	require.NoError(t, err)
	require.Same(t, current, restored)
}
