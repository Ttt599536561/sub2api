package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestWelfareAdminStatisticsRepositorySnapshotAndExactMoney(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo, ok := any(NewWelfareRepository(db)).(service.WelfareStatisticsRepository)
	require.True(t, ok, "repository must read immutable reward facts")
	mock.ExpectBegin()
	cols := []string{"date", "daily", "streak", "draw", "checkin_users", "checkin_count", "streak_users", "draw_users", "draw_count", "participants"}
	mock.ExpectQuery("GROUP BY GROUPING SETS").WithArgs("2026-10-08", "2026-10-09").WillReturnRows(sqlmock.NewRows(cols).
		AddRow("", "30", "60", "100", 2, 2, 1, 1, 2, 2).
		AddRow("2026-10-08", "30", "60", "100", 2, 2, 1, 1, 2, 2))
	mock.ExpectCommit()
	result, err := repo.AdminStatistics(context.Background(), service.WelfareStatisticsFilter{DateFrom: "2026-10-08", DateTo: "2026-10-09"})
	require.NoError(t, err)
	require.Equal(t, "1.90", result.Summary.TotalAmount)
	require.Equal(t, "0.90", result.Summary.CheckinAmount)
	require.EqualValues(t, 2, result.Summary.ParticipatingUsers)
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestWelfareAdminStatisticsRepositoryRollbackOnQueryFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo, ok := any(NewWelfareRepository(db)).(service.WelfareStatisticsRepository)
	require.True(t, ok)
	mock.ExpectBegin()
	mock.ExpectQuery("GROUP BY GROUPING SETS").WillReturnError(context.DeadlineExceeded)
	mock.ExpectRollback()
	_, err = repo.AdminStatistics(context.Background(), service.WelfareStatisticsFilter{DateFrom: "2026-10-08", DateTo: "2026-10-09"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestWelfareAdminStatisticsRepositoryLiteralSearchAndLargeAmount(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := NewWelfareRepository(db).(service.WelfareStatisticsRepository)
	mock.ExpectBegin()
	cols := []string{"date", "daily", "streak", "draw", "checkin_users", "checkin_count", "streak_users", "draw_users", "draw_count", "participants"}
	mock.ExpectQuery(`ILIKE \$3`).WithArgs("2026-10-08", "2026-10-09", `%a\%\_\\' OR 1=1%`).WillReturnRows(sqlmock.NewRows(cols).AddRow("", "18446744073709551614", "0", "1", 1, 2, 0, 1, 1, 1))
	mock.ExpectCommit()
	result, err := repo.AdminStatistics(context.Background(), service.WelfareStatisticsFilter{DateFrom: "2026-10-08", DateTo: "2026-10-09", Search: `a%_\' OR 1=1`})
	require.NoError(t, err)
	require.Equal(t, "184467440737095516.15", result.Summary.TotalAmount)
	require.NoError(t, mock.ExpectationsWereMet())
}
