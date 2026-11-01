package admin

import (
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/gin-gonic/gin"
	"time"
)

func adminPaymentOrderResponse(c *gin.Context, order *dbent.PaymentOrder) *AdminPaymentOrderResult {
	result := sanitizeAdminPaymentOrderForResponse(order)
	if result != nil && adminaccess.FromContext(c.Request.Context()) != nil {
		result.RechargeCode = ""
		// Raw fulfillment errors can contain database values or payment credentials.
		result.FailedReason = nil
	}
	return result
}

func adminPaymentOrdersResponse(c *gin.Context, orders []*dbent.PaymentOrder) []*AdminPaymentOrderResult {
	out := make([]*AdminPaymentOrderResult, 0, len(orders))
	for _, order := range orders {
		if result := adminPaymentOrderResponse(c, order); result != nil {
			out = append(out, result)
		}
	}
	return out
}

type scopedPaymentAuditResponse struct {
	ID        int64     `json:"id"`
	OrderID   string    `json:"order_id"`
	Action    string    `json:"action"`
	Operator  string    `json:"operator"`
	CreatedAt time.Time `json:"created_at"`
}

func adminPaymentAuditLogsResponse(c *gin.Context, logs []*dbent.PaymentAuditLog) any {
	if adminaccess.FromContext(c.Request.Context()) == nil {
		return logs
	}
	out := make([]scopedPaymentAuditResponse, 0, len(logs))
	for _, entry := range logs {
		if entry == nil {
			continue
		}
		// Detail is arbitrary JSON/text and can include rechargeCode. Project only
		// audit metadata, so malformed or future detail formats cannot leak secrets.
		out = append(out, scopedPaymentAuditResponse{ID: entry.ID, OrderID: entry.OrderID, Action: entry.Action, Operator: entry.Operator, CreatedAt: entry.CreatedAt})
	}
	return out
}
