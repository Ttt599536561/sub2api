package handler

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type welfareStatisticsHandlerService interface {
	AdminStatistics(context.Context, service.WelfareStatisticsFilter) (*service.WelfareStatistics, error)
	AdminStatisticsUsers(context.Context, service.WelfareStatisticsUsersFilter) (*service.WelfareStatisticsUsers, error)
	AdminStatisticsRecords(context.Context, service.WelfareStatisticsRecordsFilter) (*service.WelfareStatisticsRecords, error)
}

func (h *WelfareHandler) statisticsService(c *gin.Context) (welfareStatisticsHandlerService, bool) {
	svc, ok := h.svc.(welfareStatisticsHandlerService)
	if !ok {
		response.ErrorFrom(c, fmt.Errorf("welfare statistics service unavailable"))
	}
	return svc, ok
}
func welfareStatisticsFilter(c *gin.Context) (service.WelfareStatisticsFilter, bool) {
	filter := service.WelfareStatisticsFilter{DateFrom: c.Query("date_from"), DateTo: c.Query("date_to"), Search: c.Query("search")}
	if values, present := c.Request.URL.Query()["user_id"]; present {
		if len(values) != 1 {
			response.ErrorFrom(c, service.ErrWelfareInvalidRequest)
			return filter, false
		}
		id, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || id < 1 {
			response.ErrorFrom(c, service.ErrWelfareInvalidRequest)
			return filter, false
		}
		filter.UserID = id
	}
	return filter, true
}
func (h *WelfareHandler) AdminStatistics(c *gin.Context) {
	if !h.admin(c) {
		return
	}
	filter, ok := welfareStatisticsFilter(c)
	if !ok {
		return
	}
	svc, ok := h.statisticsService(c)
	if !ok {
		return
	}
	result, err := svc.AdminStatistics(c.Request.Context(), filter)
	welfareResult(c, result, err)
}
func (h *WelfareHandler) AdminStatisticsUsers(c *gin.Context) {
	if !h.admin(c) {
		return
	}
	common, ok := welfareStatisticsFilter(c)
	if !ok {
		return
	}
	page, ok := welfarePage(c, "page", 1, 1000000)
	if !ok {
		return
	}
	size, ok := welfarePage(c, "page_size", 20, 100)
	if !ok {
		return
	}
	filter := service.WelfareStatisticsUsersFilter{WelfareStatisticsFilter: common, Page: page, PageSize: size, SortBy: c.DefaultQuery("sort_by", "total_amount"), SortOrder: c.DefaultQuery("sort_order", "desc")}
	svc, ok := h.statisticsService(c)
	if !ok {
		return
	}
	result, err := svc.AdminStatisticsUsers(c.Request.Context(), filter)
	welfareResult(c, result, err)
}
func (h *WelfareHandler) AdminStatisticsRecords(c *gin.Context) {
	if !h.admin(c) {
		return
	}
	common, ok := welfareStatisticsFilter(c)
	if !ok {
		return
	}
	page, ok := welfarePage(c, "page", 1, 1000000)
	if !ok {
		return
	}
	size, ok := welfarePage(c, "page_size", 20, 100)
	if !ok {
		return
	}
	filter := service.WelfareStatisticsRecordsFilter{WelfareStatisticsFilter: common, Page: page, PageSize: size, Type: c.DefaultQuery("type", "all")}
	svc, ok := h.statisticsService(c)
	if !ok {
		return
	}
	result, err := svc.AdminStatisticsRecords(c.Request.Context(), filter)
	welfareResult(c, result, err)
}
