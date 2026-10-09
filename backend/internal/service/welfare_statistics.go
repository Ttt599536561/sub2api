package service

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// WelfareStatisticsFilter selects immutable reward facts by their Shanghai business date.
type WelfareStatisticsFilter struct {
	DateFrom string
	DateTo   string
	Search   string
	UserID   int64
}
type WelfareStatisticsUsersFilter struct {
	WelfareStatisticsFilter
	Page      int
	PageSize  int
	SortBy    string
	SortOrder string
}
type WelfareStatisticsRecordsFilter struct {
	WelfareStatisticsFilter
	Page     int
	PageSize int
	Type     string
}
type WelfareRewardTotals struct {
	DailyAmount        string `json:"daily_amount"`
	StreakAmount       string `json:"streak_amount"`
	CheckinAmount      string `json:"checkin_amount"`
	DrawAmount         string `json:"draw_amount"`
	TotalAmount        string `json:"total_amount"`
	CheckinUsers       int64  `json:"checkin_users"`
	CheckinCount       int64  `json:"checkin_count"`
	StreakUsers        int64  `json:"streak_users"`
	DrawUsers          int64  `json:"draw_users"`
	DrawCount          int64  `json:"draw_count"`
	ParticipatingUsers int64  `json:"participating_users"`
}
type WelfareStatisticsDay struct {
	WelfareRewardTotals
	Date string `json:"date"`
}
type WelfareStatistics struct {
	DateFrom string                 `json:"date_from"`
	DateTo   string                 `json:"date_to"`
	Timezone string                 `json:"timezone"`
	Summary  WelfareRewardTotals    `json:"summary"`
	Daily    []WelfareStatisticsDay `json:"daily"`
}
type WelfareStatisticsUser struct {
	UserID   int64               `json:"user_id"`
	Email    string              `json:"email"`
	Period   WelfareRewardTotals `json:"period"`
	Lifetime WelfareRewardTotals `json:"lifetime"`
}
type WelfareStatisticsUsers struct {
	Items    []WelfareStatisticsUser `json:"items"`
	Total    int64                   `json:"total"`
	Page     int                     `json:"page"`
	PageSize int                     `json:"page_size"`
}
type WelfareStatisticsRecord struct {
	ID           string    `json:"id"`
	UserID       int64     `json:"user_id"`
	Email        string    `json:"email"`
	Type         string    `json:"type"`
	CreatedAt    time.Time `json:"created_at"`
	BusinessDate string    `json:"business_date"`
	Amount       string    `json:"amount"`
	CycleDay     *int64    `json:"cycle_day,omitempty"`
}
type WelfareStatisticsRecords struct {
	Items    []WelfareStatisticsRecord `json:"items"`
	Total    int64                     `json:"total"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"page_size"`
}

// This optional interface keeps existing welfare mutation repositories and mocks unchanged.
type WelfareStatisticsRepository interface {
	AdminStatistics(context.Context, WelfareStatisticsFilter) (*WelfareStatistics, error)
	AdminStatisticsUsers(context.Context, WelfareStatisticsUsersFilter) (*WelfareStatisticsUsers, error)
	AdminStatisticsRecords(context.Context, WelfareStatisticsRecordsFilter) (*WelfareStatisticsRecords, error)
}

func (s *WelfareService) statisticsFilter(filter WelfareStatisticsFilter) (WelfareStatisticsFilter, error) {
	if filter.UserID < 0 {
		return filter, ErrWelfareInvalidRequest
	}
	today := s.now().In(welfareShanghai).Format("2006-01-02")
	if filter.DateTo == "" {
		filter.DateTo = today
	}
	to, err := time.ParseInLocation("2006-01-02", filter.DateTo, welfareShanghai)
	if err != nil {
		return filter, ErrWelfareInvalidRequest
	}
	if filter.DateFrom == "" {
		filter.DateFrom = to.AddDate(0, 0, -6).Format("2006-01-02")
	}
	from, err := time.ParseInLocation("2006-01-02", filter.DateFrom, welfareShanghai)
	if err != nil || from.Year() < 1 || from.After(to) || to.After(from.AddDate(0, 0, 365)) {
		return filter, ErrWelfareInvalidRequest
	}
	filter.Search = strings.TrimSpace(filter.Search)
	return filter, nil
}
func (s *WelfareService) statisticsRepository() (WelfareStatisticsRepository, error) {
	repo, ok := s.repo.(WelfareStatisticsRepository)
	if !ok {
		return nil, fmt.Errorf("welfare statistics repository unavailable")
	}
	return repo, nil
}
func zeroWelfareRewardTotals() WelfareRewardTotals {
	return WelfareRewardTotals{DailyAmount: "0.00", StreakAmount: "0.00", CheckinAmount: "0.00", DrawAmount: "0.00", TotalAmount: "0.00"}
}
func (s *WelfareService) AdminStatistics(ctx context.Context, filter WelfareStatisticsFilter) (*WelfareStatistics, error) {
	filter, err := s.statisticsFilter(filter)
	if err != nil {
		return nil, err
	}
	repo, err := s.statisticsRepository()
	if err != nil {
		return nil, err
	}
	result, err := repo.AdminStatistics(ctx, filter)
	if err != nil {
		return nil, err
	}
	byDate := make(map[string]WelfareStatisticsDay, len(result.Daily))
	for _, day := range result.Daily {
		byDate[day.Date] = day
	}
	result.DateFrom, result.DateTo, result.Timezone = filter.DateFrom, filter.DateTo, "Asia/Shanghai"
	result.Daily = make([]WelfareStatisticsDay, 0, 366)
	from, _ := time.ParseInLocation("2006-01-02", filter.DateFrom, welfareShanghai)
	to, _ := time.ParseInLocation("2006-01-02", filter.DateTo, welfareShanghai)
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		date := day.Format("2006-01-02")
		value, ok := byDate[date]
		if !ok {
			value = WelfareStatisticsDay{Date: date, WelfareRewardTotals: zeroWelfareRewardTotals()}
		}
		result.Daily = append(result.Daily, value)
	}
	return result, nil
}
func statisticsPagination(page, size int) (int, int, error) {
	if page < 0 || page > 1000000 || size < 0 || size > 100 {
		return 0, 0, ErrWelfareInvalidRequest
	}
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	return page, size, nil
}
func (s *WelfareService) AdminStatisticsUsers(ctx context.Context, filter WelfareStatisticsUsersFilter) (*WelfareStatisticsUsers, error) {
	var err error
	filter.WelfareStatisticsFilter, err = s.statisticsFilter(filter.WelfareStatisticsFilter)
	if err != nil {
		return nil, err
	}
	filter.Page, filter.PageSize, err = statisticsPagination(filter.Page, filter.PageSize)
	if err != nil {
		return nil, err
	}
	if filter.SortBy == "" {
		filter.SortBy = "total_amount"
	}
	if filter.SortOrder == "" {
		filter.SortOrder = "desc"
	}
	if (filter.SortBy != "total_amount" && filter.SortBy != "period_total_amount" && filter.SortBy != "checkin_count") || (filter.SortOrder != "asc" && filter.SortOrder != "desc") {
		return nil, ErrWelfareInvalidRequest
	}
	repo, err := s.statisticsRepository()
	if err != nil {
		return nil, err
	}
	result, err := repo.AdminStatisticsUsers(ctx, filter)
	if err != nil {
		return nil, err
	}
	if result.Items == nil {
		result.Items = []WelfareStatisticsUser{}
	}
	result.Page, result.PageSize = filter.Page, filter.PageSize
	return result, nil
}
func (s *WelfareService) AdminStatisticsRecords(ctx context.Context, filter WelfareStatisticsRecordsFilter) (*WelfareStatisticsRecords, error) {
	var err error
	filter.WelfareStatisticsFilter, err = s.statisticsFilter(filter.WelfareStatisticsFilter)
	if err != nil {
		return nil, err
	}
	filter.Page, filter.PageSize, err = statisticsPagination(filter.Page, filter.PageSize)
	if err != nil {
		return nil, err
	}
	if filter.Type == "" {
		filter.Type = "all"
	}
	if filter.Type != "all" && filter.Type != "daily" && filter.Type != "streak" && filter.Type != "draw" {
		return nil, ErrWelfareInvalidRequest
	}
	repo, err := s.statisticsRepository()
	if err != nil {
		return nil, err
	}
	result, err := repo.AdminStatisticsRecords(ctx, filter)
	if err != nil {
		return nil, err
	}
	if result.Items == nil {
		result.Items = []WelfareStatisticsRecord{}
	}
	result.Page, result.PageSize = filter.Page, filter.PageSize
	return result, nil
}
