package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// scopedAdminBalanceHistoryResponse exposes only ledger fields and relation IDs.
// Related users/groups are read through their own interfaces even when the key
// also grants users.read/groups.read. New general DTO fields are not inherited.
func scopedAdminBalanceHistoryResponse(record service.RedeemCode) gin.H {
	// Clear relations on a copy before constructing the DTO; never mutate the
	// service result or construct nested financial/user/group configuration data.
	record.User, record.Group = nil, nil
	entry := dto.RedeemCodeFromServiceAdmin(&record)
	out := gin.H{
		"id":            entry.ID,
		"code":          entry.Code,
		"type":          entry.Type,
		"value":         entry.Value,
		"status":        entry.Status,
		"used_by":       entry.UsedBy,
		"used_at":       entry.UsedAt,
		"created_at":    entry.CreatedAt,
		"group_id":      entry.GroupID,
		"validity_days": entry.ValidityDays,
		"notes":         entry.Notes,
	}
	if entry.ExpiresAt != nil {
		out["expires_at"] = entry.ExpiresAt
	}
	return out
}

func adminBalanceHistoryResponses(c *gin.Context, records []service.RedeemCode) any {
	if adminaccess.FromContext(c.Request.Context()) != nil {
		out := make([]gin.H, 0, len(records))
		for i := range records {
			out = append(out, scopedAdminBalanceHistoryResponse(records[i]))
		}
		return out
	}
	// JWT administrators and legacy unrestricted keys retain the existing DTO.
	out := make([]dto.AdminRedeemCode, 0, len(records))
	for i := range records {
		out = append(out, *dto.RedeemCodeFromServiceAdmin(&records[i]))
	}
	return out
}
