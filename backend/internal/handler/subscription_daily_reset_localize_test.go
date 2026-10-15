package handler

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDailyResetResponseLocalization(t *testing.T) {
	out := &service.DailyResetOutcome{
		OperationID: "reset-operation", Replayed: true, ResetPerformed: true,
		PreferenceSaved: true, CheckError: "quota_check_pending",
		Subscription: &service.UserSubscription{
			ID: 7, GroupID: 2,
			Group: &service.Group{
				ID: 2, Name: "月卡", Description: "每日额度",
				AllowSubscriptionDayReset: true,
				I18n:                      service.GroupI18n{"en": {Name: "Monthly plan", Description: "Daily quota"}},
			},
		},
	}
	before, err := json.Marshal(out)
	require.NoError(t, err)
	for _, tc := range []struct {
		language, name, description string
	}{
		{language: "en-US,en;q=0.9", name: "Monthly plan", description: "Daily quota"},
		{language: "zh", name: "月卡", description: "每日额度"},
		{language: "fr", name: "月卡", description: "每日额度"},
		{language: "", name: "月卡", description: "每日额度"},
	} {
		t.Run(tc.language, func(t *testing.T) {
			got := resetResponse(out, tc.language)
			require.Equal(t, tc.name, got.Subscription.Group.Name)
			require.Equal(t, tc.description, got.Subscription.Group.Description)
			require.True(t, got.Subscription.Group.AllowSubscriptionDayReset)
			require.Equal(t, out.OperationID, got.OperationID)
			require.Equal(t, out.Replayed, got.Replayed)
			require.Equal(t, out.ResetPerformed, got.ResetPerformed)
			require.Equal(t, out.PreferenceSaved, got.PreferenceSaved)
			require.Equal(t, out.CheckError, got.CheckError)
			raw, err := json.Marshal(got)
			require.NoError(t, err)
			var decoded struct {
				Subscription struct {
					Group map[string]any `json:"group"`
				} `json:"subscription"`
			}
			require.NoError(t, json.Unmarshal(raw, &decoded))
			require.NotContains(t, decoded.Subscription.Group, "i18n")
			after, err := json.Marshal(out)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after), "localization must leave the service outcome unchanged")
		})
	}
}
