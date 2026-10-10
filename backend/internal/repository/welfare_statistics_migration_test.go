package repository

import (
	"context"
	"database/sql"
	"testing"
	"testing/fstest"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestWelfareStatisticsMigrationRecoversInvalidIndexBeforeRetry(t *testing.T) {
	const name = "242_welfare_statistics_business_date_idx_notx.sql"
	const index = "welfare_ledger_reward_business_date_idx"
	migration, err := migrations.FS.ReadFile(name)
	require.NoError(t, err)
	for _, invalid := range []bool{true, false} {
		label := "preserve valid index"
		if invalid {
			label = "rebuild failed concurrent index"
		}
		t.Run(label, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			prepareMigrationsBootstrapExpectations(mock)
			mock.ExpectQuery(`SELECT checksum FROM schema_migrations WHERE filename = \$1`).WithArgs(name).WillReturnError(sql.ErrNoRows)
			mock.ExpectQuery(`SELECT EXISTS \(`).WithArgs(index).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(invalid))
			if invalid {
				mock.ExpectExec(`DROP INDEX CONCURRENTLY IF EXISTS ` + index).WillReturnResult(sqlmock.NewResult(0, 0))
			}
			mock.ExpectExec(`CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + index).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec(`INSERT INTO schema_migrations \(filename, checksum\) VALUES \(\$1, \$2\)`).WithArgs(name, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec(`SELECT pg_advisory_unlock\(\$1\)`).WithArgs(migrationsAdvisoryLockID).WillReturnResult(sqlmock.NewResult(0, 1))
			fsys := fstest.MapFS{name: &fstest.MapFile{Data: migration}}
			require.NoError(t, applyMigrationsFS(context.Background(), db, fsys))
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
