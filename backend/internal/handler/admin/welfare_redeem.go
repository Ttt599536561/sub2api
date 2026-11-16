package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// WelfareCampaigns shares the existing redeem management permission surface.
func (h *RedeemHandler) WelfareCampaigns(c *gin.Context) {
	options, err := h.redeemService.ListWelfareRedeemCampaigns(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, options)
}
