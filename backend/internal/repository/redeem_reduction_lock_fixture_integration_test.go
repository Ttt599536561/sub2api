//go:build integration

package repository

import (
	"database/sql"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
)

// Keep the integration suite on its original Docker-backed shared fixture.
func reductionLockFixtureDatabase(t *testing.T) (*dbent.Client, *sql.DB) {
	t.Helper()
	return testEntClient(t), integrationDB
}
