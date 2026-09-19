//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func welfareSpendFixture(t *testing.T) (service.UsageBillingRepository, int64, int64) {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t)
	_, err := integrationDB.ExecContext(ctx, `UPDATE welfare_programs SET enabled=true,launch_at=NOW()-interval '1 hour' WHERE id=1`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = integrationDB.Exec(`UPDATE welfare_programs SET enabled=false WHERE id=1`) })
	u := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("welfare-spend-%d@example.com", time.Now().UnixNano()), Balance: 200})
	k := mustCreateApiKey(t, client, &service.APIKey{UserID: u.ID, Key: "welfare-" + uuid.NewString(), Name: "welfare"})
	return NewUsageBillingRepository(client, integrationDB), u.ID, k.ID
}

func TestWelfareSpendCrossesThresholdSynchronouslyAndDeduplicates(t *testing.T) {
	repo, u, k := welfareSpendFixture(t)
	ctx := context.Background()
	first := &service.UsageBillingCommand{RequestID: uuid.NewString(), UserID: u, APIKeyID: k, BalanceCost: 49.99999999}
	_, err := repo.Apply(ctx, first)
	require.NoError(t, err)
	second := &service.UsageBillingCommand{RequestID: uuid.NewString(), UserID: u, APIKeyID: k, BalanceCost: .00000001}
	_, err = repo.Apply(ctx, second)
	require.NoError(t, err)
	replay, err := repo.Apply(ctx, second)
	require.NoError(t, err)
	require.False(t, replay.Applied)
	var spend, balance string
	var tickets, version, balanceVersion, events int64
	require.NoError(t, integrationDB.QueryRow(`SELECT eligible_spend::text,floor(eligible_spend/50)::bigint-draws_used,wallet_version,welfare_balance_version FROM welfare_wallets WHERE user_id=$1`, u).Scan(&spend, &tickets, &version, &balanceVersion))
	require.Equal(t, "50.00000000", spend)
	require.EqualValues(t, 1, tickets)
	require.EqualValues(t, 2, version)
	require.Zero(t, balanceVersion)
	require.NoError(t, integrationDB.QueryRow(`SELECT balance::text FROM users WHERE id=$1`, u).Scan(&balance))
	require.Equal(t, "150.00000000", balance)
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM welfare_spend_events WHERE user_id=$1`, u).Scan(&events))
	require.EqualValues(t, 2, events)
	_, err = integrationDB.Exec(`INSERT INTO usage_billing_dedup_archive(request_id,api_key_id,request_fingerprint,created_at) SELECT request_id,api_key_id,request_fingerprint,created_at FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, second.RequestID, k)
	require.NoError(t, err)
	_, err = integrationDB.Exec(`DELETE FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, second.RequestID, k)
	require.NoError(t, err)
	replay, err = repo.Apply(ctx, second)
	require.NoError(t, err)
	require.False(t, replay.Applied)
}

func TestWelfareSpendRollbackAndDisabledAccrual(t *testing.T) {
	repo, u, k := welfareSpendFixture(t)
	ctx := context.Background()
	_, err := repo.Apply(ctx, &service.UsageBillingCommand{RequestID: uuid.NewString(), UserID: u, APIKeyID: -999, BalanceCost: 50, APIKeyQuotaCost: 50})
	require.Error(t, err)
	var events int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM welfare_spend_events WHERE user_id=$1`, u).Scan(&events))
	require.Zero(t, events)
	_, err = integrationDB.Exec(`UPDATE welfare_programs SET enabled=false WHERE id=1`)
	require.NoError(t, err)
	_, err = repo.Apply(ctx, &service.UsageBillingCommand{RequestID: uuid.NewString(), UserID: u, APIKeyID: k, BalanceCost: 50})
	require.NoError(t, err)
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM welfare_spend_events WHERE user_id=$1`, u).Scan(&events))
	require.Zero(t, events)
}

func TestWelfareSpendBatchOnlyCapturedActualIsEligible(t *testing.T) {
	repo, u, k := welfareSpendFixture(t)
	ctx := context.Background()
	batch := uuid.NewString()
	_, err := repo.ReserveBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageHoldRequestID(batch), BatchID: batch, UserID: u, APIKeyID: k, HoldAmount: 60})
	require.NoError(t, err)
	var events int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM welfare_spend_events WHERE user_id=$1`, u).Scan(&events))
	require.Zero(t, events)
	_, err = repo.CaptureBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{RequestID: uuid.NewString(), BatchID: batch, UserID: u, APIKeyID: k, HoldAmount: 60, ActualAmount: 50})
	require.NoError(t, err)
	var spend string
	require.NoError(t, integrationDB.QueryRow(`SELECT eligible_spend::text FROM welfare_wallets WHERE user_id=$1`, u).Scan(&spend))
	require.Equal(t, "50.00000000", spend)
}

func TestWelfareSpendBatchQuantizationMatchesActualDebit(t *testing.T) {
	repo, u, k := welfareSpendFixture(t)
	ctx := context.Background()
	batch := uuid.NewString()
	_, err := repo.ReserveBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageHoldRequestID(batch), BatchID: batch, UserID: u, APIKeyID: k, HoldAmount: 1})
	require.NoError(t, err)
	_, err = repo.CaptureBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{RequestID: uuid.NewString(), BatchID: batch, UserID: u, APIKeyID: k, HoldAmount: 1, ActualAmount: .000078125})
	require.NoError(t, err)
	var matches bool
	require.NoError(t, integrationDB.QueryRow(`SELECT (200-u.balance)=w.eligible_spend FROM users u JOIN welfare_wallets w ON w.user_id=u.id WHERE u.id=$1`, u).Scan(&matches))
	require.True(t, matches)
}

func TestWelfareSpendRefundIsIdempotentBoundedAndRetainsTicketDebt(t *testing.T) {
	repo, u, k := welfareSpendFixture(t)
	actor := mustCreateUser(t, testEntClient(t), &service.User{Email: "welfare-refund-admin-" + uuid.NewString() + "@example.com", Role: service.RoleAdmin})
	ctx := context.Background()
	request := uuid.NewString()
	_, err := repo.Apply(ctx, &service.UsageBillingCommand{RequestID: request, UserID: u, APIKeyID: k, BalanceCost: 50})
	require.NoError(t, err)
	_, err = integrationDB.Exec(`UPDATE welfare_wallets SET draws_used=1 WHERE user_id=$1`, u)
	require.NoError(t, err)
	source := welfareUsageSourceID(request, k)
	refund := func(id string, amount float64) (bool, error) {
		tx, err := integrationDB.BeginTx(ctx, nil)
		if err != nil {
			return false, err
		}
		defer func() { _ = tx.Rollback() }()
		applied, err := RefundWelfareUsageBalance(ctx, tx, u, "usage", source, id, amount, actor.ID, "Correct duplicate upstream charge")
		if err != nil {
			return false, err
		}
		return applied, tx.Commit()
	}
	applied, err := refund("refund-1", 1)
	require.NoError(t, err)
	require.True(t, applied)
	applied, err = refund("refund-1", 1)
	require.NoError(t, err)
	require.False(t, applied)
	_, err = refund("refund-1", 2)
	require.Error(t, err)
	_, err = refund("refund-overflow", 50)
	require.Error(t, err)
	var spend, balance string
	var debt, events int64
	require.NoError(t, integrationDB.QueryRow(`SELECT eligible_spend::text,draws_used-floor(eligible_spend/50)::bigint FROM welfare_wallets WHERE user_id=$1`, u).Scan(&spend, &debt))
	require.Equal(t, "49.00000000", spend)
	require.EqualValues(t, 1, debt)
	require.NoError(t, integrationDB.QueryRow(`SELECT balance::text FROM users WHERE id=$1`, u).Scan(&balance))
	require.Equal(t, "151.00000000", balance)
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM welfare_balance_outbox WHERE user_id=$1`, u).Scan(&events))
	require.EqualValues(t, 1, events)
}

func TestWelfareSpendRefundRequiresAuditableAdminAndReason(t *testing.T) {
	repo, userID, keyID := welfareSpendFixture(t)
	ctx := context.Background()
	actor := mustCreateUser(t, testEntClient(t), &service.User{Email: "welfare-audit-admin-" + uuid.NewString() + "@example.com", Role: service.RoleAdmin})
	otherActor := mustCreateUser(t, testEntClient(t), &service.User{Email: "welfare-audit-other-" + uuid.NewString() + "@example.com", Role: service.RoleAdmin})
	inactiveActor := mustCreateUser(t, testEntClient(t), &service.User{Email: "welfare-audit-inactive-" + uuid.NewString() + "@example.com", Role: service.RoleAdmin, Status: service.StatusDisabled})
	deletedActor := mustCreateUser(t, testEntClient(t), &service.User{Email: "welfare-audit-deleted-" + uuid.NewString() + "@example.com", Role: service.RoleAdmin})
	_, err := integrationDB.Exec(`UPDATE users SET deleted_at=NOW() WHERE id=$1`, deletedActor.ID)
	require.NoError(t, err)
	requestID := uuid.NewString()
	_, err = repo.Apply(ctx, &service.UsageBillingCommand{RequestID: requestID, UserID: userID, APIKeyID: keyID, BalanceCost: 50})
	require.NoError(t, err)
	source := welfareUsageSourceID(requestID, keyID)
	refund := func(refundID string, actorID int64, reason string) (bool, error) {
		tx, err := integrationDB.BeginTx(ctx, nil)
		if err != nil {
			return false, err
		}
		defer func() { _ = tx.Rollback() }()
		applied, err := RefundWelfareUsageBalance(ctx, tx, userID, "usage", source, refundID, 1, actorID, reason)
		if err != nil {
			return false, err
		}
		return applied, tx.Commit()
	}
	for _, tc := range []struct {
		name    string
		actorID int64
		reason  string
	}{
		{"missing_actor", 0, "Accounting correction"},
		{"negative_actor", -1, "Accounting correction"},
		{"unknown_actor", 9223372036854775807, "Accounting correction"},
		{"non_admin", userID, "Accounting correction"},
		{"inactive_admin", inactiveActor.ID, "Accounting correction"},
		{"deleted_admin", deletedActor.ID, "Accounting correction"},
		{"missing_reason", actor.ID, ""},
		{"blank_reason", actor.ID, " \t\n "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			applied, err := refund(tc.name, tc.actorID, tc.reason)
			require.Error(t, err)
			require.False(t, applied)
		})
	}
	// The database also protects manual SQL callers from unaudited facts.
	_, err = integrationDB.Exec(`INSERT INTO welfare_spend_events(user_id,source_type,source_id,action,amount,refund_source_id) VALUES($1,'usage',$2,'refund',-1,$3)`, userID, "sql-missing-metadata-"+uuid.NewString(), source)
	require.Error(t, err)
	_, err = integrationDB.Exec(`INSERT INTO welfare_spend_events(user_id,source_type,source_id,action,amount,refund_source_id,actor_id,reason) VALUES($1,'usage',$2,'refund',-1,$3,9223372036854775807,'Accounting correction')`, userID, "sql-unknown-actor-"+uuid.NewString(), source)
	require.Error(t, err)
	var balance string
	var refundCount, outboxCount int
	require.NoError(t, integrationDB.QueryRow(`SELECT balance::text FROM users WHERE id=$1`, userID).Scan(&balance))
	require.Equal(t, "150.00000000", balance)
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM welfare_spend_events WHERE user_id=$1 AND action='refund'`, userID).Scan(&refundCount))
	require.Zero(t, refundCount)
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM welfare_balance_outbox WHERE user_id=$1`, userID).Scan(&outboxCount))
	require.Zero(t, outboxCount)
	applied, err := refund("audited-refund", actor.ID, "  Correct duplicate upstream charge  ")
	require.NoError(t, err)
	require.True(t, applied)
	var recordedActor int64
	var recordedReason string
	require.NoError(t, integrationDB.QueryRow(`SELECT actor_id,reason FROM welfare_spend_events WHERE user_id=$1 AND action='refund'`, userID).Scan(&recordedActor, &recordedReason))
	require.Equal(t, actor.ID, recordedActor)
	require.Equal(t, "Correct duplicate upstream charge", recordedReason)
	_, err = integrationDB.Exec(`UPDATE welfare_spend_events SET reason='Rewrite audit history' WHERE user_id=$1 AND action='refund'`, userID)
	require.Error(t, err)
	applied, err = refund("audited-refund", actor.ID, recordedReason)
	require.NoError(t, err)
	require.False(t, applied)
	_, err = refund("audited-refund", otherActor.ID, recordedReason)
	require.ErrorContains(t, err, "idempotency conflict")
	_, err = refund("audited-refund", actor.ID, "Different correction reason")
	require.ErrorContains(t, err, "idempotency conflict")
	require.NoError(t, integrationDB.QueryRow(`SELECT balance::text FROM users WHERE id=$1`, userID).Scan(&balance))
	require.Equal(t, "151.00000000", balance)
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM welfare_spend_events WHERE user_id=$1 AND action='refund'`, userID).Scan(&refundCount))
	require.Equal(t, 1, refundCount)
	var debitMetadataAbsent bool
	require.NoError(t, integrationDB.QueryRow(`SELECT actor_id IS NULL AND reason IS NULL FROM welfare_spend_events WHERE user_id=$1 AND action='debit'`, userID).Scan(&debitMetadataAbsent))
	require.True(t, debitMetadataAbsent)
}

func TestWelfareSpendRefundReciprocalAdminsUseConsistentLockOrder(t *testing.T) {
	repo, userID, keyID := welfareSpendFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := integrationDB.ExecContext(ctx, `UPDATE users SET role=$2 WHERE id=$1`, userID, service.RoleAdmin)
	require.NoError(t, err)
	other := mustCreateUser(t, testEntClient(t), &service.User{Email: "welfare-cross-refund-" + uuid.NewString() + "@example.com", Role: service.RoleAdmin, Balance: 200})
	otherKey := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: other.ID, Key: "welfare-" + uuid.NewString(), Name: "welfare"})
	users := []int64{userID, other.ID}
	keys := []int64{keyID, otherKey.ID}
	sources := make([]string, 2)
	for i := range users {
		requestID := uuid.NewString()
		_, err := repo.Apply(ctx, &service.UsageBillingCommand{RequestID: requestID, UserID: users[i], APIKeyID: keys[i], BalanceCost: 50})
		require.NoError(t, err)
		sources[i] = welfareUsageSourceID(requestID, keys[i])
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := range users {
		go func(i int) {
			<-start
			for iteration := 0; iteration < 10; iteration++ {
				tx, err := integrationDB.BeginTx(ctx, nil)
				if err != nil {
					results <- err
					return
				}
				_, err = RefundWelfareUsageBalance(ctx, tx, users[i], "usage", sources[i], uuid.NewString(), 0.01, users[1-i], "Reviewed accounting correction")
				if err != nil {
					_ = tx.Rollback()
					results <- err
					return
				}
				if err := tx.Commit(); err != nil {
					results <- err
					return
				}
			}
			results <- nil
		}(i)
	}
	close(start)
	require.NoError(t, <-results)
	require.NoError(t, <-results)
}
