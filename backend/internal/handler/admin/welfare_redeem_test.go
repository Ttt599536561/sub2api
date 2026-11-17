package admin

import (
	"bytes"
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

type welfareGenerateAdmin struct {
	service.AdminService
	input *service.GenerateRedeemCodesInput
}

func (s *welfareGenerateAdmin) GenerateRedeemCodes(_ context.Context, input *service.GenerateRedeemCodesInput) ([]service.RedeemCode, error) {
	s.input = input
	return []service.RedeemCode{}, nil
}

func TestGenerateWelfareSupplementPassesExplicitContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &welfareGenerateAdmin{}
	h := NewRedeemHandler(stub, nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/redeem-codes/generate", bytes.NewBufferString(`{"count":2,"type":"subscription","subscription_effect":"add_quota","welfare_campaign_id":7,"quota_amount_usd":12.34}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.Generate(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotNil(t, stub.input)
	require.Equal(t, "add_quota", stub.input.SubscriptionEffect)
	require.EqualValues(t, 7, *stub.input.WelfareCampaignID)
	require.Equal(t, 12.34, *stub.input.QuotaAmountUSD)
	require.Nil(t, stub.input.GroupID)
}

func TestGenerateSubscriptionSupplementAcceptsGroupWithoutCampaign(t *testing.T) {
	for _, body := range []string{
		`{"count":2,"type":"subscription","group_id":5,"subscription_effect":"add_balance","quota_amount_usd":12.34}`,
		`{"count":2,"type":"subscription","group_id":5,"subscription_effect":"extend_subscription","validity_days":7}`,
	} {
		stub := &welfareGenerateAdmin{}
		h := NewRedeemHandler(stub, nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/redeem-codes/generate", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		h.Generate(c)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.NotNil(t, stub.input)
		require.EqualValues(t, 5, *stub.input.GroupID)
		require.Nil(t, stub.input.WelfareCampaignID)
	}
}
