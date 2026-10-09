package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type welfareStatisticsRepoStub struct {
	WelfareRepository
	filter        WelfareStatisticsFilter
	usersFilter   WelfareStatisticsUsersFilter
	recordsFilter WelfareStatisticsRecordsFilter
	calls         int
}

func (r *welfareStatisticsRepoStub) AdminStatistics(_ context.Context, f WelfareStatisticsFilter) (*WelfareStatistics, error) {
	r.filter = f
	r.calls++
	return &WelfareStatistics{Summary: WelfareRewardTotals{TotalAmount: "1.90"}, Daily: []WelfareStatisticsDay{{Date: "2026-10-08", WelfareRewardTotals: WelfareRewardTotals{TotalAmount: "1.90"}}}}, nil
}
func (r *welfareStatisticsRepoStub) AdminStatisticsUsers(_ context.Context, f WelfareStatisticsUsersFilter) (*WelfareStatisticsUsers, error) {
	r.usersFilter = f
	r.calls++
	return &WelfareStatisticsUsers{}, nil
}
func (r *welfareStatisticsRepoStub) AdminStatisticsRecords(_ context.Context, f WelfareStatisticsRecordsFilter) (*WelfareStatisticsRecords, error) {
	r.recordsFilter = f
	r.calls++
	return &WelfareStatisticsRecords{}, nil
}

func statisticsTestService(r *welfareStatisticsRepoStub) *WelfareService {
	// UTC Oct 8 17:00 is already Oct 9 in Shanghai.
	return NewWelfareService(r, WithWelfareClock(func() time.Time { return time.Date(2026, 10, 8, 17, 0, 0, 0, time.UTC) }))
}
func TestWelfareAdminStatisticsDefaultsToShanghaiWeekAndFillsDays(t *testing.T) {
	r := &welfareStatisticsRepoStub{}
	s := statisticsTestService(r)
	result, err := s.AdminStatistics(context.Background(), WelfareStatisticsFilter{})
	require.NoError(t, err)
	require.Equal(t, "2026-10-03", r.filter.DateFrom)
	require.Equal(t, "2026-10-09", r.filter.DateTo)
	require.Equal(t, "Asia/Shanghai", result.Timezone)
	require.Equal(t, r.filter.DateFrom, result.DateFrom)
	require.Len(t, result.Daily, 7)
	require.Equal(t, "1.90", result.Summary.TotalAmount)
	require.Equal(t, "1.90", result.Daily[5].TotalAmount)
	require.Equal(t, "2026-10-09", result.Daily[6].Date)
	require.Equal(t, "0.00", result.Daily[6].TotalAmount)
	require.Equal(t, "0.00", result.Daily[6].CheckinAmount)
}
func TestWelfareAdminStatisticsRejectsInvalidDatesAndFilters(t *testing.T) {
	cases := []WelfareStatisticsFilter{
		{DateFrom: "2026-02-30"}, {DateFrom: "0000-01-01", DateTo: "0000-01-01"}, {DateTo: "2026-10-09T00:00:00Z"}, {DateFrom: "2026-10-10", DateTo: "2026-10-09"},
		{DateFrom: "2025-10-08", DateTo: "2026-10-09"}, {UserID: -1},
	}
	for _, f := range cases {
		r := &welfareStatisticsRepoStub{}
		_, err := statisticsTestService(r).AdminStatistics(context.Background(), f)
		require.ErrorIs(t, err, ErrWelfareInvalidRequest)
		require.Zero(t, r.calls)
	}
	r := &welfareStatisticsRepoStub{}
	_, err := statisticsTestService(r).AdminStatistics(context.Background(), WelfareStatisticsFilter{DateFrom: "2025-10-09", DateTo: "2026-10-09"})
	require.NoError(t, err, "366 inclusive days are allowed")
}
func TestWelfareAdminStatisticsListsValidateDefaultsAndEmptyArrays(t *testing.T) {
	r := &welfareStatisticsRepoStub{}
	s := statisticsTestService(r)
	users, err := s.AdminStatisticsUsers(context.Background(), WelfareStatisticsUsersFilter{})
	require.NoError(t, err)
	require.Equal(t, "total_amount", r.usersFilter.SortBy)
	require.Equal(t, "desc", r.usersFilter.SortOrder)
	require.Equal(t, 1, users.Page)
	require.Equal(t, 20, users.PageSize)
	require.NotNil(t, users.Items)
	records, err := s.AdminStatisticsRecords(context.Background(), WelfareStatisticsRecordsFilter{})
	require.NoError(t, err)
	require.Equal(t, "all", r.recordsFilter.Type)
	require.NotNil(t, records.Items)
	for _, f := range []WelfareStatisticsUsersFilter{{Page: -1}, {Page: 1000001}, {PageSize: 101}, {SortBy: "email"}, {SortOrder: "up"}} {
		_, err = s.AdminStatisticsUsers(context.Background(), f)
		require.ErrorIs(t, err, ErrWelfareInvalidRequest)
	}
	for _, f := range []WelfareStatisticsRecordsFilter{{PageSize: -1}, {PageSize: 101}, {Type: "redeem"}} {
		_, err = s.AdminStatisticsRecords(context.Background(), f)
		require.ErrorIs(t, err, ErrWelfareInvalidRequest)
	}
}
