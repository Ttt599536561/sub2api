//go:build integration

package repository

import "testing"

// The deployed custom v0.2.8-r1 has all 292 migrations in ebaa8c022, including
// the three custom migrations. Upgrade that exact database history, preserving
// paid-reset events, welfare balances/facts/rewards, orders and cache outbox,
// while applying the current migrations and checking another startup. The shared
// assertions require the new group i18n column to default to {} without changing
// any fields already present in this pinned historical schema.
func TestSubscriptionDailyResetUpgradeV0213_PreservesDeployedV028StateAndHistory(t *testing.T) {
	testDeployedCustomUpgrade(t, customV028MigrationBaseline)
}
