//go:build integration

package repository

import "testing"

// The deployed custom v0.2.8-r1 has all 292 migrations in ebaa8c022, including
// the three custom migrations. Upgrade that exact database history, preserving
// paid-reset events, welfare balances/facts/rewards, orders and cache outbox,
// while applying the two official 241 migrations and checking another startup.
func TestSubscriptionDailyResetUpgradeV0213_PreservesDeployedV028StateAndHistory(t *testing.T) {
	testDeployedCustomUpgrade(t, customV028MigrationBaseline)
}
