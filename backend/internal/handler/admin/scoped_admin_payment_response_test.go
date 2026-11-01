//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPaymentReadResponseHidesRechargeCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, detail := range []string{`{"rechargeCode":"PAY-REVIEW-ONLY"}`, `broken PAY-REVIEW-ONLY audit detail`} {
		for _, scoped := range []bool{false, true} {
			for _, endpoint := range []string{"list", "detail"} {
				t.Run(endpoint+"/"+map[bool]string{true: "scoped", false: "full"}[scoped]+"/"+detail, func(t *testing.T) {
					db, mock, err := sqlmock.New()
					require.NoError(t, err)
					client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
					t.Cleanup(func() { _ = client.Close() })
					if endpoint == "list" {
						mock.ExpectQuery(`SELECT COUNT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
					}
					mock.ExpectQuery(`SELECT .* FROM "payment_orders"`).WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "recharge_code", "amount", "status", "failed_reason"}).AddRow(123, 456, "PAY-REVIEW-ONLY", 10, "FAILED", "PAY-REVIEW-ONLY error"))
					if endpoint == "detail" {
						mock.ExpectQuery(`SELECT .* FROM "payment_audit_logs"`).WillReturnRows(sqlmock.NewRows([]string{"id", "order_id", "action", "detail", "operator", "created_at"}).AddRow(1, "123", "RECHARGE_SUCCESS", detail, "system", time.Now()))
					}
					h := NewPaymentHandler(service.NewPaymentService(client, nil, nil, nil, nil, nil, nil, nil, nil), nil)
					router := gin.New()
					router.Use(func(c *gin.Context) {
						if scoped {
							c.Request = c.Request.WithContext(adminaccess.WithPrincipal(c.Request.Context(), &adminaccess.Principal{Policy: adminaccess.Policy{GlobalPermissions: []string{"payment.read"}}}))
						}
						c.Next()
					})
					router.GET("/orders", h.ListOrders)
					router.GET("/orders/:id", h.GetOrderDetail)
					path := "/orders"
					if endpoint == "detail" {
						path += "/123"
					}
					w := httptest.NewRecorder()
					router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
					require.Equal(t, 200, w.Code, w.Body.String())
					require.NoError(t, mock.ExpectationsWereMet())
					if scoped {
						require.NotContains(t, w.Body.String(), "PAY-REVIEW-ONLY")
						require.NotContains(t, w.Body.String(), `"recharge_code"`)
						require.NotContains(t, w.Body.String(), `"failed_reason"`)
						if endpoint == "detail" {
							require.NotContains(t, w.Body.String(), `"detail"`)
							require.Contains(t, w.Body.String(), "RECHARGE_SUCCESS")
						}
					} else {
						require.Contains(t, w.Body.String(), "PAY-REVIEW-ONLY")
					}
				})
			}
		}
	}
}

type reservedPaymentAdminRepo struct {
	service.RedeemCodeRepository
	lookups, creates int
}

func (r *reservedPaymentAdminRepo) GetByCode(context.Context, string) (*service.RedeemCode, error) {
	r.lookups++
	return nil, service.ErrRedeemCodeNotFound
}
func (r *reservedPaymentAdminRepo) Create(context.Context, *service.RedeemCode) error {
	r.creates++
	return nil
}
func TestAdminCreateAndRedeemRejectsPaymentNamespaceBeforeWrites(t *testing.T) {
	for _, code := range []string{"PAY-REVIEW-ONLY", "  PAY-REVIEW-ONLY  "} {
		t.Run(code, func(t *testing.T) {
			repo := &reservedPaymentAdminRepo{}
			h := &RedeemHandler{redeemService: service.NewRedeemService(repo, nil, nil, nil, nil, nil, nil, nil)}
			body, err := json.Marshal(map[string]any{"code": code, "value": 10, "user_id": 42})
			require.NoError(t, err)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("POST", "/api/v1/admin/redeem-codes/create-and-redeem", strings.NewReader(string(body)))
			c.Request.Header.Set("Content-Type", "application/json")
			h.CreateAndRedeem(c)
			require.Equal(t, 403, rec.Code, rec.Body.String())
			require.Zero(t, repo.creates)
			require.Zero(t, repo.lookups)
		})
	}
}
