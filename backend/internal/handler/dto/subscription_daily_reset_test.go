package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionDailyResetJSONMatchesClientContract(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	sub := &service.UserSubscription{ID: 1, UserID: 2, GroupID: 3, DailyResetState: &service.DailyResetState{
		Eligible: true, CanReset: true, DailyResetCount: 4, DailyResetLimit: 100, DailyResetVersion: 9,
		CountDate: "2026-09-06", ServerTime: now,
	}}
	body, err := json.Marshal(UserSubscriptionFromService(sub))
	require.NoError(t, err)
	var wire map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &wire))
	require.Contains(t, wire, "daily_reset")
	var state map[string]any
	require.NoError(t, json.Unmarshal(wire["daily_reset"], &state))
	require.Equal(t, float64(4), state["today_reset_count"])
	require.Equal(t, "2026-09-06", state["server_date"])
	require.Equal(t, float64(9), state["daily_reset_version"])
	require.Equal(t, true, state["can_reset"])
	require.NotContains(t, wire, "daily_reset_state")
}
