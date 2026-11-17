//go:build unit

package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestGrokVideoBillingTaskRepositoryPreservesScopeAndIdentity(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := newUsageLogRepositoryWithSQL(nil, db)
	payload := []byte(`{"subscription_binding":{"subscription_id":91,"term_starts_at":"2026-11-01T00:00:00Z"}}`)
	mock.ExpectExec("INSERT INTO grok_video_billing_tasks").WithArgs("task", int64(10), int64(20), string(payload)).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.StoreGrokVideoBillingTask(context.Background(), "task", 10, 20, payload))
	mock.ExpectQuery("SELECT snapshot FROM grok_video_billing_tasks WHERE task_id=\\$1 AND user_id=\\$2 AND api_key_id=\\$3").WithArgs("task", int64(10), int64(20)).WillReturnRows(sqlmock.NewRows([]string{"snapshot"}).AddRow(payload))
	got, err := repo.LoadGrokVideoBillingTask(context.Background(), "task", 10, 20)
	require.NoError(t, err)
	require.JSONEq(t, string(payload), string(got))
	mock.ExpectQuery("SELECT snapshot FROM grok_video_billing_tasks").WithArgs("task", int64(11), int64(20)).WillReturnRows(sqlmock.NewRows([]string{"snapshot"}))
	got, err = repo.LoadGrokVideoBillingTask(context.Background(), "task", 11, 20)
	require.NoError(t, err)
	require.Nil(t, got)
	mock.ExpectExec("INSERT INTO grok_video_billing_tasks").WithArgs("task", int64(10), int64(20), string(payload)).WillReturnResult(sqlmock.NewResult(0, 0))
	require.Error(t, repo.StoreGrokVideoBillingTask(context.Background(), "task", 10, 20, payload), "conflicting identity must not silently overwrite")
	require.NoError(t, mock.ExpectationsWereMet())
}
