package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/qqinvite"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestQQInviteCampaignGroupLockedError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		err     error
		code    string
		message string
	}{
		{"group_locked", qqinvite.ErrCampaignGroupLocked, "QQ_INVITE_CAMPAIGN_GROUP_LOCKED", "The subscription group can only be changed for a disabled campaign that has never been enabled and has no code inventory or invitation history"},
		{"wrapped_group_locked", fmt.Errorf("save campaign: %w", qqinvite.ErrCampaignGroupLocked), "QQ_INVITE_CAMPAIGN_GROUP_LOCKED", "The subscription group can only be changed for a disabled campaign that has never been enabled and has no code inventory or invitation history"},
		{"stale_version", qqinvite.ErrConflict, "QQ_INVITE_CONFLICT", "The record changed or conflicts with current activity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			qqInviteResult(c, nil, tc.err)
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			var body struct {
				Code    int    `json:"code"`
				Reason  string `json:"reason"`
				Message string `json:"message"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.Equal(t, http.StatusConflict, body.Code)
			require.Equal(t, tc.code, body.Reason)
			require.Equal(t, tc.message, body.Message)
		})
	}
}
