//go:build integration

package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestWelfareBalanceOutboxLeaseFencesStaleAcknowledgement(t *testing.T) {
	_, user, _ := welfareSpendFixture(t)
	ctx := context.Background()
	var id int64
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO welfare_balance_outbox(user_id,version)VALUES($1,1)RETURNING id`, user).Scan(&id))
	repo := NewWelfareBalanceOutboxRepository(integrationDB)
	find := func(events []service.WelfareBalanceOutboxEvent) service.WelfareBalanceOutboxEvent {
		for _, e := range events {
			if e.ID == id {
				return e
			}
		}
		t.Fatal("event not claimed")
		return service.WelfareBalanceOutboxEvent{}
	}
	events, err := repo.Claim(ctx, 10000, time.Minute)
	require.NoError(t, err)
	old := find(events)
	events, err = repo.Claim(ctx, 10000, time.Minute)
	require.NoError(t, err)
	for _, e := range events {
		require.NotEqual(t, id, e.ID)
	}
	_, err = integrationDB.Exec(`UPDATE welfare_balance_outbox SET lease_until=NOW()-interval '1 second' WHERE id=$1`, id)
	require.NoError(t, err)
	events, err = repo.Claim(ctx, 10000, time.Minute)
	require.NoError(t, err)
	current := find(events)
	require.NotEqual(t, old.LeaseToken, current.LeaseToken)
	require.NoError(t, repo.Complete(ctx, old))
	var completed bool
	require.NoError(t, integrationDB.QueryRow(`SELECT completed_at IS NOT NULL FROM welfare_balance_outbox WHERE id=$1`, id).Scan(&completed))
	require.False(t, completed)
	require.NoError(t, repo.Retry(ctx, current, "redis unavailable", 0))
	events, err = repo.Claim(ctx, 10000, time.Minute)
	require.NoError(t, err)
	next := find(events)
	require.Equal(t, 3, next.Attempts)
	require.NoError(t, repo.Complete(ctx, next))
	require.NoError(t, integrationDB.QueryRow(`SELECT completed_at IS NOT NULL FROM welfare_balance_outbox WHERE id=$1`, id).Scan(&completed))
	require.True(t, completed)
}
