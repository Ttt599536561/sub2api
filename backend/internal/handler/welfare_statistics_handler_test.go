package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type welfareStatisticsHandlerRepo struct {
	service.WelfareRepository
	filter service.WelfareStatisticsFilter
	calls  int
}

func (r *welfareStatisticsHandlerRepo) AdminStatistics(_ context.Context, f service.WelfareStatisticsFilter) (*service.WelfareStatistics, error) {
	r.filter = f
	r.calls++
	return &service.WelfareStatistics{}, nil
}
func (r *welfareStatisticsHandlerRepo) AdminStatisticsUsers(_ context.Context, f service.WelfareStatisticsUsersFilter) (*service.WelfareStatisticsUsers, error) {
	r.filter = f.WelfareStatisticsFilter
	r.calls++
	return &service.WelfareStatisticsUsers{}, nil
}
func (r *welfareStatisticsHandlerRepo) AdminStatisticsRecords(_ context.Context, f service.WelfareStatisticsRecordsFilter) (*service.WelfareStatisticsRecords, error) {
	r.filter = f.WelfareStatisticsFilter
	r.calls++
	return &service.WelfareStatisticsRecords{}, nil
}
func statisticsHandler(r *welfareStatisticsHandlerRepo) *WelfareHandler {
	return NewWelfareHandler(service.NewWelfareService(r, service.WithWelfareClock(func() time.Time { return time.Date(2026, 10, 8, 17, 0, 0, 0, time.UTC) })), nil, nil)
}
func TestWelfareAdminStatisticsHTTPRequiresAdministrator(t *testing.T) {
	r := &welfareStatisticsHandlerRepo{}
	h := statisticsHandler(r)
	for _, handle := range []gin.HandlerFunc{h.AdminStatistics, h.AdminStatisticsUsers, h.AdminStatisticsRecords} {
		w := welfareRequest(h, "GET", "/welfare", "", "", 0, false, handle)
		require.Equal(t, http.StatusUnauthorized, w.Code)
		w = welfareRequest(h, "GET", "/welfare", "", "", 42, false, handle)
		require.Equal(t, http.StatusForbidden, w.Code)
		require.Zero(t, r.calls)
		w = welfareRequest(h, "GET", "/welfare", "", "", 42, true, handle)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		r.calls = 0
	}
}
func TestWelfareAdminStatisticsHTTPFiltersAndValidation(t *testing.T) {
	r := &welfareStatisticsHandlerRepo{}
	h := statisticsHandler(r)
	w := welfareRequest(h, "GET", "/welfare?date_from=2026-10-08&date_to=2026-10-09&user_id=13&search=a%25_b", "", "", 42, true, h.AdminStatistics)
	require.Equal(t, http.StatusOK, w.Code)
	require.EqualValues(t, 13, r.filter.UserID)
	require.Equal(t, "a%_b", r.filter.Search)
	for _, path := range []string{"/welfare?user_id=0", "/welfare?user_id=-1", "/welfare?user_id=1.2", "/welfare?user_id=999999999999999999999", "/welfare?date_from=bad", "/welfare?date_from=2026-10-10&date_to=2026-10-09"} {
		w = welfareRequest(h, "GET", path, "", "", 42, true, h.AdminStatistics)
		require.Equal(t, http.StatusBadRequest, w.Code, path)
	}
	for _, path := range []string{"/welfare?page=0", "/welfare?page_size=101", "/welfare?page_size=-1", "/welfare?sort_by=email", "/welfare?sort_order=up"} {
		w = welfareRequest(h, "GET", path, "", "", 42, true, h.AdminStatisticsUsers)
		require.Equal(t, http.StatusBadRequest, w.Code, path)
	}
	w = welfareRequest(h, "GET", "/welfare?type=redeem", "", "", 42, true, h.AdminStatisticsRecords)
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = welfareRequest(h, "GET", "/welfare?page=3&page_size=10&type=draw", "", "", 42, true, h.AdminStatisticsRecords)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"items":[]`)
}
