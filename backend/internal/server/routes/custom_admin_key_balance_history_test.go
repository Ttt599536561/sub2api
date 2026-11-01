//go:build unit

package routes

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type balanceHistoryRouteService struct {
	service.AdminService
	records  []service.RedeemCode
	userID   int64
	page     int
	pageSize int
	codeType string
	calls    int
}

func (s *balanceHistoryRouteService) GetUserBalanceHistory(_ context.Context, userID int64, page, pageSize int, codeType string) ([]service.RedeemCode, int64, float64, error) {
	s.userID, s.page, s.pageSize, s.codeType = userID, page, pageSize, codeType
	s.calls++
	return s.records, int64(len(s.records)), 42.5, nil
}

type balanceHistoryRouteSettings struct{ service.SettingRepository }

func (*balanceHistoryRouteSettings) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}

func (*balanceHistoryRouteSettings) GetValue(_ context.Context, key string) (string, error) {
	if key == service.SettingKeyAdminAPIKey {
		return "legacy-balance-history-key", nil
	}
	return "", service.ErrSettingNotFound
}

type balanceHistoryRouteUsers struct{ scopedRouteUsers }

func (r *balanceHistoryRouteUsers) GetFirstAdmin(ctx context.Context) (*service.User, error) {
	return r.GetByID(ctx, 9)
}

func balanceHistoryRouteFixture() []service.RedeemCode {
	groupID, userID := int64(99), int64(7)
	usedAt := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	expiresAt := usedAt.Add(24 * time.Hour)
	return []service.RedeemCode{{
		ID: 1, Code: "used-ledger-code", Type: "subscription", Value: 12.5, Status: service.StatusUsed,
		UsedBy: &userID, UsedAt: &usedAt, CreatedAt: usedAt.Add(-time.Hour), ExpiresAt: &expiresAt,
		GroupID: &groupID, ValidityDays: 30, Notes: "ledger-adjustment-note",
		Group: &service.Group{ID: groupID, Name: "private-group", IsExclusive: true, RateMultiplier: 17.25},
		User:  &service.User{ID: userID, Email: "private-user@example.invalid", Balance: 98765},
	}}
}

// Exercise real key/JWT authentication, registered routes and the real handler.
// Only persistence and the history service result are replaced by local fixtures.
func newBalanceHistoryRoute(t *testing.T, history *balanceHistoryRouteService, identity string, permissions []string) (*gin.Engine, string, string) {
	t.Helper()
	settings := service.NewSettingService(&balanceHistoryRouteSettings{}, nil)
	settings.SetScopedAdminKeyRepository(&scopedRouteKeys{})
	users := service.NewUserService(&balanceHistoryRouteUsers{}, nil, nil, nil)
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "balance-history-test-secret", ExpireHour: 1}}
	authService := service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	header, credential := "x-api-key", "legacy-balance-history-key"
	switch identity {
	case "scoped":
		key, _, err := settings.CreateScopedAdminKey(context.Background(), service.ScopedAdminKeyInput{
			Name: "balance-history-test", Enabled: true, Policy: adminaccess.Policy{GlobalPermissions: permissions},
		}, 9)
		require.NoError(t, err)
		credential = key
	case "jwt":
		token, err := authService.GenerateToken(context.Background(), &service.User{ID: 9, Role: service.RoleAdmin})
		require.NoError(t, err)
		header, credential = "Authorization", "Bearer "+token
	}
	auth := middleware.NewAdminAuthMiddleware(authService, users, settings, nil)
	r := gin.New()
	RegisterAdminRoutes(r.Group("/api/v1"), &handler.Handlers{Admin: &handler.AdminHandlers{
		Account: &adminhandler.AccountHandler{},
		User:    adminhandler.NewUserHandler(history, nil, nil, nil, nil, nil, nil),
		Setting: adminhandler.NewSettingHandler(settings, nil, nil, nil, nil, nil, nil),
	}}, auth, middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() }), middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() }), nil, nil)
	return r, header, credential
}

func TestScopedAdminBalanceHistoryDoesNotGrantNestedResourceRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name        string
		permissions []string
	}{
		{"balance_only", []string{"users.balance.read"}},
		{"with_groups_read", []string{"users.balance.read", "groups.read"}},
		{"with_user_and_group_read", []string{"users.balance.read", "users.read", "groups.read"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, filter := range []string{"", "subscription"} {
				t.Run("filter_"+filter, func(t *testing.T) {
					history := &balanceHistoryRouteService{records: balanceHistoryRouteFixture()}
					r, header, credential := newBalanceHistoryRoute(t, history, "scoped", tc.permissions)
					if tc.name == "balance_only" {
						denied := httptest.NewRecorder()
						req := httptest.NewRequest("GET", "/api/v1/admin/groups/99", nil)
						req.Header.Set(header, credential)
						r.ServeHTTP(denied, req)
						require.Equal(t, 403, denied.Code)
					}
					w := httptest.NewRecorder()
					req := httptest.NewRequest("GET", "/api/v1/admin/users/7/balance-history?page=2&page_size=5&type="+filter, nil)
					req.Header.Set(header, credential)
					r.ServeHTTP(w, req)
					require.Equal(t, 200, w.Code, w.Body.String())
					var result struct {
						Data struct {
							Items          []map[string]json.RawMessage `json:"items"`
							Total          int64                        `json:"total"`
							Page           int                          `json:"page"`
							PageSize       int                          `json:"page_size"`
							Pages          int                          `json:"pages"`
							TotalRecharged float64                      `json:"total_recharged"`
						} `json:"data"`
					}
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
					require.Len(t, result.Data.Items, 1)
					item := result.Data.Items[0]
					// Even a separately granted groups.read/users.read does not expand
					// this ledger endpoint; related resources use their own interfaces.
					require.NotContains(t, item, "group")
					require.NotContains(t, item, "user")
					require.NotContains(t, w.Body.String(), "private-")
					original, err := json.Marshal(dto.RedeemCodeFromServiceAdmin(&history.records[0]))
					require.NoError(t, err)
					var expected map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(original, &expected))
					delete(expected, "group")
					delete(expected, "user")
					require.Equal(t, expected, item, "keep the ledger's own fields and relation IDs")
					require.EqualValues(t, 1, result.Data.Total)
					require.Equal(t, 2, result.Data.Page)
					require.Equal(t, 5, result.Data.PageSize)
					require.Equal(t, 1, result.Data.Pages)
					require.Equal(t, 42.5, result.Data.TotalRecharged)
					require.EqualValues(t, 7, history.userID)
					require.Equal(t, filter, history.codeType)
					require.Equal(t, 2, history.page)
					require.Equal(t, 5, history.pageSize)
					require.Equal(t, 17.25, history.records[0].Group.RateMultiplier, "projection must not mutate the source")
					require.Equal(t, "private-user@example.invalid", history.records[0].User.Email)
				})
			}
		})
	}
}

func TestScopedAdminBalanceHistoryRequiresBalanceRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	history := &balanceHistoryRouteService{records: balanceHistoryRouteFixture()}
	r, header, credential := newBalanceHistoryRoute(t, history, "scoped", []string{"users.read", "groups.read", "subscriptions.read"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/users/7/balance-history?type=subscription", nil)
	req.Header.Set(header, credential)
	r.ServeHTTP(w, req)
	require.Equal(t, 403, w.Code)
	require.Zero(t, history.calls)
}

func TestAdminBalanceHistoryFullIdentityCompatibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, identity := range []string{"jwt", "legacy"} {
		t.Run(identity, func(t *testing.T) {
			history := &balanceHistoryRouteService{records: balanceHistoryRouteFixture()}
			r, header, credential := newBalanceHistoryRoute(t, history, identity, nil)
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/api/v1/admin/users/7/balance-history", nil)
			req.Header.Set(header, credential)
			r.ServeHTTP(w, req)
			require.Equal(t, 200, w.Code, w.Body.String())
			var result struct {
				Data struct {
					Items []json.RawMessage `json:"items"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
			require.Len(t, result.Data.Items, 1)
			expected, err := json.Marshal(dto.RedeemCodeFromServiceAdmin(&history.records[0]))
			require.NoError(t, err)
			require.JSONEq(t, string(expected), string(result.Data.Items[0]))
		})
	}
}

func TestAdminBalanceHistoryEmptyRemainsArray(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, identity := range []string{"scoped", "jwt", "legacy"} {
		t.Run(identity, func(t *testing.T) {
			r, header, credential := newBalanceHistoryRoute(t, &balanceHistoryRouteService{}, identity, []string{"users.balance.read"})
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/api/v1/admin/users/7/balance-history", nil)
			req.Header.Set(header, credential)
			r.ServeHTTP(w, req)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"items":[]`)
		})
	}
}
