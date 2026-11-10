package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/qqinvite"
	"github.com/gin-gonic/gin"
)

func (h *QQInviteHandler) Campaigns(c *gin.Context) {
	if _, ok := h.authorize(c, false); !ok {
		return
	}
	v, err := h.store.ListCampaigns(c.Request.Context())
	qqInviteResult(c, v, err)
}
func (h *QQInviteHandler) SaveCampaign(c *gin.Context) {
	actor, ok := h.authorize(c, true)
	if !ok {
		return
	}
	var v qqinvite.Campaign
	if qqImageDecode(c, &v) != nil {
		qqInviteResult(c, nil, qqinvite.ErrInvalid)
		return
	}
	if c.Param("id") != "" {
		id, ok := inviteParam(c)
		if !ok {
			return
		}
		if v.ID != 0 && v.ID != id {
			qqInviteResult(c, nil, qqinvite.ErrInvalid)
			return
		}
		v.ID = id
	} else if v.ID != 0 {
		qqInviteResult(c, nil, qqinvite.ErrInvalid)
		return
	}
	saved, err := h.store.SaveCampaign(c.Request.Context(), v, actor)
	qqInviteResult(c, saved, err)
}
func (h *QQInviteHandler) Baseline(c *gin.Context) {
	actor, ok := h.authorize(c, true)
	if !ok {
		return
	}
	id, ok := inviteParam(c)
	if !ok {
		return
	}
	if h.baseline == nil {
		qqInviteResult(c, nil, qqinvite.ErrDisabled)
		return
	}
	err := h.baseline(c.Request.Context(), id, actor)
	qqInviteResult(c, map[string]bool{"baseline_ready": err == nil}, err)
}
func (h *QQInviteHandler) Codes(c *gin.Context) {
	if _, ok := h.authorize(c, false); !ok {
		return
	}
	id, ok := inviteParam(c)
	if !ok {
		return
	}
	p, n := invitePage(c)
	v, err := h.store.ListCodes(c.Request.Context(), id, p, n)
	qqInviteResult(c, v, err)
}
func (h *QQInviteHandler) codeImport(c *gin.Context, preview bool) {
	actor, ok := h.authorize(c, true)
	if !ok {
		return
	}
	id, ok := inviteParam(c)
	if !ok {
		return
	}
	var body struct {
		Codes []string `json:"codes"`
	}
	if qqImageDecode(c, &body) != nil || len(body.Codes) == 0 || len(body.Codes) > 500 {
		qqInviteResult(c, nil, qqinvite.ErrInvalid)
		return
	}
	var v qqinvite.CodePreview
	var err error
	if preview {
		v, err = h.store.PreviewCodes(c.Request.Context(), id, body.Codes)
	} else {
		v, err = h.store.ImportCodes(c.Request.Context(), id, body.Codes, actor)
	}
	qqInviteResult(c, v, err)
}
func (h *QQInviteHandler) PreviewCodes(c *gin.Context) { h.codeImport(c, true) }
func (h *QQInviteHandler) ImportCodes(c *gin.Context)  { h.codeImport(c, false) }
func (h *QQInviteHandler) GenerateCodes(c *gin.Context) {
	actor, ok := h.authorize(c, true)
	if !ok {
		return
	}
	id, ok := inviteParam(c)
	if !ok {
		return
	}
	var body struct {
		Count int `json:"count"`
	}
	if qqImageDecode(c, &body) != nil || body.Count < 1 || body.Count > 500 {
		qqInviteResult(c, nil, qqinvite.ErrInvalid)
		return
	}
	v, err := h.store.GenerateCodes(c.Request.Context(), id, body.Count, actor)
	qqInviteResult(c, v, err)
}
func (h *QQInviteHandler) Records(c *gin.Context) {
	if _, ok := h.authorize(c, false); !ok {
		return
	}
	id, ok := inviteParam(c)
	if !ok {
		return
	}
	p, n := invitePage(c)
	v, err := h.store.ListRecords(c.Request.Context(), id, c.Query("status"), p, n)
	qqInviteResult(c, v, err)
}
func (h *QQInviteHandler) Recheck(c *gin.Context) {
	actor, ok := h.authorize(c, true)
	if !ok {
		return
	}
	id, ok := inviteParam(c)
	if !ok {
		return
	}
	qqInviteResult(c, map[string]bool{"queued": true}, h.store.RecheckRecord(c.Request.Context(), id, actor))
}
func (h *QQInviteHandler) Review(c *gin.Context) {
	actor, ok := h.authorize(c, true)
	if !ok {
		return
	}
	id, ok := inviteParam(c)
	if !ok {
		return
	}
	var body struct {
		InviterQQ string `json:"inviter_qq"`
		Reason    string `json:"reason"`
	}
	if qqImageDecode(c, &body) != nil {
		qqInviteResult(c, nil, qqinvite.ErrInvalid)
		return
	}
	qqInviteResult(c, map[string]bool{"queued": true}, h.store.ReviewRecord(c.Request.Context(), id, body.InviterQQ, body.Reason, actor))
}
func (h *QQInviteHandler) Redeliver(c *gin.Context) {
	actor, ok := h.authorize(c, true)
	if !ok {
		return
	}
	id, ok := inviteParam(c)
	if !ok {
		return
	}
	qqInviteResult(c, map[string]bool{"queued": true}, h.store.RedeliverReward(c.Request.Context(), id, actor))
}
