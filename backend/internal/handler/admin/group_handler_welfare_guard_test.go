package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type welfareGuardAdminService struct{ service.AdminService }

func (s *welfareGuardAdminService) UpdateGroup(context.Context, int64, *service.UpdateGroupInput) (*service.Group, error) {
	return nil, service.ErrGroupWelfareReferenced.WithMetadata(map[string]string{"group_id": "1", "campaign_count": "2"})
}
func (s *welfareGuardAdminService) DeleteGroup(context.Context, int64) error {
	return service.ErrGroupWelfareReferenced.WithMetadata(map[string]string{"group_id": "1", "campaign_count": "2"})
}

func TestGroupHandlerWelfareProtectionReturnsConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewGroupHandler(&welfareGuardAdminService{}, nil, nil)
	r := gin.New()
	r.PUT("/groups/:id", h.Update)
	r.DELETE("/groups/:id", h.Delete)
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/groups/1", bytes.NewBufferString(`{"subscription_type":"standard"}`))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			r.ServeHTTP(res, req)
			require.Equal(t, http.StatusConflict, res.Code)
			require.Contains(t, res.Body.String(), "GROUP_WELFARE_REFERENCED")
			require.Contains(t, res.Body.String(), "cannot be deleted")
			require.Contains(t, res.Body.String(), `"metadata":{"campaign_count":"2","group_id":"1"}`)
		})
	}
}
